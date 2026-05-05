// Package agent contains the AI logic backed by Firebase Genkit (supports Gemini and other providers)
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"hairy-botter/internal/ai/domain"
	"hairy-botter/internal/rag"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	genkitMCP "github.com/firebase/genkit/go/plugins/mcp"
)

// MCPServer defines the configuration for a single MCP server.
type MCPServer struct {
	Type string
	Path string
	Args []string
	Env  map[string]string
}

type historyLogic interface {
	Read(ctx context.Context, sessionID string) ([]*ai.Message, error)
	Save(ctx context.Context, sessionID string, history []*ai.Message) error
}

type contextKey string

// Logic .
type Logic struct {
	logger *slog.Logger

	g       *genkit.Genkit
	model   ai.Model
	history historyLogic
	persona string

	toolRefs  []ai.ToolRef
	toolNames []string
	extraOpts []ai.GenerateOption

	// RAG related fields
	ragL *rag.Logic
}

// ToolNames returns the names of every tool this agent has access to.
// Used by the MCP server layer to advertise capabilities to orchestrators.
func (l *Logic) ToolNames() []string {
	return l.toolNames
}

// Persona returns the effective system prompt (role + system_prompt from config.yaml, plus any auto-injected context).
func (l *Logic) Persona() string {
	return l.persona
}

// New .
func New(logger *slog.Logger, g *genkit.Genkit, model ai.Model, history historyLogic, inputMCPServers []MCPServer, ragL *rag.Logic, persona string, extraOpts []ai.GenerateOption) (*Logic, error) {
	var tools []ai.Tool

	if len(inputMCPServers) > 0 {
		mcpServers := make([]genkitMCP.MCPServerConfig, 0, len(inputMCPServers))
		for i, srv := range inputMCPServers {
			cfg := genkitMCP.MCPServerConfig{
				Name: fmt.Sprintf("mcp-client-%d", i), // Unique name for each client
			}

			serverType := srv.Type
			if serverType == "" {
				if len(srv.Path) >= 4 && srv.Path[:4] == "http" {
					serverType = "http"
				} else {
					serverType = "cli"
				}
			}

			if serverType == "http" {
				cfg.Config = genkitMCP.MCPClientOptions{
					StreamableHTTP: &genkitMCP.StreamableHTTPConfig{
						BaseURL: srv.Path,
						Timeout: 15 * time.Second,
					},
				}
			} else if serverType == "cli" {
				if srv.Path == "" {
					return nil, errors.New("empty cli path for mcp server")
				}

				// Build the environment
				var env []string
				// 1. Get from OS environment
				for _, e := range os.Environ() {
					env = append(env, e)
				}
				// 2. Override with config environment
				if len(srv.Env) > 0 {
					for k, v := range srv.Env {
						env = append(env, fmt.Sprintf("%s=%s", k, v))
					}
				}

				cfg.Config = genkitMCP.MCPClientOptions{
					Stdio: &genkitMCP.StdioConfig{
						Command: srv.Path,
						Args:    srv.Args,
						Env:     env,
					},
				}
			} else {
				logger.Warn("unknown mcp server type", slog.String("type", serverType))
				continue
			}

			mcpServers = append(mcpServers, cfg)
		}

		logger.Warn("initializing MCP clients", slog.Int("count", len(mcpServers)))

		// NewMCPHost blocks synchronously for each server (connect + initialize) using
		// context.Background() internally, so we run it in a goroutine with a hard timeout
		// to prevent an unreachable server from hanging startup indefinitely.
		type hostResult struct {
			host *genkitMCP.MCPHost
		}
		hostCh := make(chan hostResult, 1)
		go func() {
			m, _ := genkitMCP.NewMCPHost(g, genkitMCP.MCPHostOptions{
				Name:       "hairy-botter-mcp-host",
				Version:    "1.0.0",
				MCPServers: mcpServers,
			})
			hostCh <- hostResult{m}
		}()

		const mcpInitTimeout = 30 * time.Second
		var mcpManager *genkitMCP.MCPHost
		select {
		case r := <-hostCh:
			mcpManager = r.host
			logger.Warn("MCP host initialized")
		case <-time.After(mcpInitTimeout):
			logger.Warn("MCP host initialization timed out, starting without MCP tools", slog.Duration("timeout", mcpInitTimeout))
		}

		if mcpManager != nil {
			logger.Warn("loading MCP tools")
			toolsCtx, toolsCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer toolsCancel()
			var toolsErr error
			tools, toolsErr = mcpManager.GetActiveTools(toolsCtx, g)
			if toolsErr != nil {
				logger.Warn("failed to get MCP tools, continuing without them", slog.String("error", toolsErr.Error()))
				tools = nil
			}
		}
	}

	// Convert ai.Tools to ai.ToolRefs; capture names before the interface erasure.
	toolRefs := make([]ai.ToolRef, len(tools))
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolRefs[i] = tool
		toolNames[i] = tool.Name()
	}
	logger.Warn("tools loaded", slog.Int("num_tools", len(toolRefs)))

	return &Logic{
		logger:    logger,
		g:         g,
		model:     model,
		history:   history,
		persona:   persona,
		toolRefs:  toolRefs,
		toolNames: toolNames,
		extraOpts: extraOpts,
		ragL:      ragL,
	}, nil
}

// HandleMessage as an internal logic
// sessionID is unique to be able to get the history
func (l *Logic) HandleMessage(ctx context.Context, sessionID string, req domain.Request) (string, error) {
	if sessionID == "" {
		return "", errors.New("sessionID is empty")
	}
	logger := l.logger.With("sessionID", sessionID)
	logger.Info("handling message", slog.String("message", req.Message))

	hist, err := l.history.Read(ctx, sessionID)
	if err != nil {
		return "", err
	}

	logger.Info("generating chat content")
	ragContextDocs := make([]*ai.Document, 0)
	if l.ragL != nil {
		logger.Info("adding RAG context to history")
		ragContent, err := l.ragL.Retrieve(ctx, &ai.RetrieverRequest{
			Query:   ai.DocumentFromText(req.Message, nil),
			Options: map[string]any{"limit": 3},
		})
		if err != nil {
			logger.Error("failed to query RAG content", slog.String("error", err.Error()))

			return "", err
		}

		// If we found content, collect it and log
		if len(ragContent.Documents) > 0 {
			ragContextDocs = ragContent.Documents
			logger.Info("RAG content found, adding to the request", slog.Int("num_results", len(ragContextDocs)))
		}
	}

	userPromptParts := make([]*ai.Part, 0, len(req.InlineData)+1)
	for _, inlineData := range req.InlineData {
		// If we have some inline data convert them to prompt parts
		userPromptParts = append(userPromptParts, ai.NewMediaPart(inlineData.MimeType, string(inlineData.Data)))
	}

	// Add the user's request at the end too
	userPromptParts = append(userPromptParts, ai.NewTextPart(req.Message))
	hist = append(hist, ai.NewUserMessage(userPromptParts...))

	logger.Debug("message parts sending to LLM", slog.Any("parts", userPromptParts))
	// TODO: We could re-use a flow here maybe, but for simplicity we create a new generate just for each message. We can optimize later if needed.

	toolLogger := logger
	genOpts := []ai.GenerateOption{
		ai.WithModel(l.model),
		ai.WithSystem(l.persona),
		ai.WithTools(l.toolRefs...),
		ai.WithToolChoice(ai.ToolChoiceAuto),
		ai.WithMessages(hist...),
		ai.WithUse(ai.MiddlewareFunc(func(ctx context.Context) (*ai.Hooks, error) {
			return &ai.Hooks{
				WrapTool: func(ctx context.Context, params *ai.ToolParams, next ai.ToolNext) (*ai.MultipartToolResponse, error) {
					toolLogger.Info("tool call", slog.String("tool", params.Request.Name), slog.Any("input", params.Request.Input))
					resp, err := next(ctx, params)
					if err != nil {
						toolLogger.Warn("tool error", slog.String("tool", params.Request.Name), slog.String("error", err.Error()))
					} else {
						toolLogger.Info("tool response", slog.String("tool", params.Request.Name), slog.Any("output", resp.Output))
					}
					return resp, err
				},
			}, nil
		})),
	}
	genOpts = append(genOpts, l.extraOpts...)

	if len(ragContextDocs) > 0 {
		genOpts = append(genOpts, ai.WithDocs(ragContextDocs...))
	}

	genCtx, genCancel := context.WithTimeout(ctx, 120*time.Second)
	defer genCancel()
	logger.Info("calling genkit.Generate", slog.String("model", l.model.Name()), slog.Int("history_len", len(hist)), slog.Int("num_tools", len(l.toolRefs)))
	resp, err := genkit.Generate(genCtx, l.g, genOpts...)
	if err != nil {
		logger.Error("genkit.Generate failed", slog.String("error", err.Error()))
		return "", err
	}
	logger.Debug("genkit.Generate succeeded", slog.Int("response_len", len(resp.Text())))

	// Save hist (already includes the user message) plus the model response.
	// Deliberately avoid resp.History() because genkit may inject RAG docs into the
	// message list before sending to the model, which would bloat saved history.
	toSave := append(hist, ai.NewModelTextMessage(resp.Text()))
	err = l.history.Save(ctx, sessionID, toSave)

	return resp.Text(), err
}
