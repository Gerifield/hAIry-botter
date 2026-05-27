// Package agent contains the AI logic backed by Firebase Genkit (supports Gemini and other providers)
package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"hairy-botter/internal/ai/domain"
	"hairy-botter/internal/config"
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

	g            *genkit.Genkit
	model        ai.Model
	history      historyLogic
	systemPrompt string

	toolRefs  []ai.ToolRef
	toolNames []string
	extraOpts []ai.GenerateOption

	// RAG related fields
	ragL *rag.Logic

	// Configured context for static and dynamic data loading
	contextConfig config.ContextConfig
}

// ToolNames returns the names of every tool this agent has access to.
// Used by the MCP server layer to advertise capabilities to orchestrators.
func (l *Logic) ToolNames() []string {
	return l.toolNames
}

// SystemPrompt returns the effective system prompt (role + system_prompt from config.yaml, plus any auto-injected context).
func (l *Logic) SystemPrompt() string {
	return l.systemPrompt
}

// New .
func New(logger *slog.Logger, g *genkit.Genkit, model ai.Model, history historyLogic, inputMCPServers []MCPServer, ragL *rag.Logic, systemPrompt string, extraOpts []ai.GenerateOption, contextConfig config.ContextConfig) (*Logic, error) {
	var tools []ai.Tool

	if len(inputMCPServers) > 0 {
		mcpServers := make([]genkitMCP.MCPServerConfig, 0, len(inputMCPServers))
		for i, srv := range inputMCPServers {
			cfg := genkitMCP.MCPServerConfig{
				Name: fmt.Sprintf("mcp-%d", i),
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

		logger.Info("initializing MCP clients", slog.Int("count", len(mcpServers)))

		mcpManager, _ := genkitMCP.NewMCPHost(g, genkitMCP.MCPHostOptions{
			Name:       "hairy-botter-mcp-host",
			Version:    "1.0.0",
			MCPServers: mcpServers,
		})
		logger.Info("MCP host initialized")

		logger.Info("loading MCP tools")
		toolsCtx, toolsCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer toolsCancel()
		var toolsErr error
		tools, toolsErr = mcpManager.GetActiveTools(toolsCtx, g)
		if toolsErr != nil {
			logger.Warn("failed to get MCP tools, continuing without them", slog.String("error", toolsErr.Error()))
			tools = nil
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
		logger:        logger,
		g:             g,
		model:         model,
		history:       history,
		systemPrompt:  systemPrompt,
		toolRefs:      toolRefs,
		toolNames:     toolNames,
		extraOpts:     extraOpts,
		ragL:          ragL,
		contextConfig: contextConfig,
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
		userPromptParts = append(userPromptParts, ai.NewMediaPart(inlineData.MimeType, "data:"+inlineData.MimeType+";base64,"+base64.StdEncoding.EncodeToString(inlineData.Data)))
	}

	// Add the user's request at the end too (skip if empty, e.g. file sent without caption)
	if req.Message != "" {
		userPromptParts = append(userPromptParts, ai.NewTextPart(req.Message))
	}
	hist = append(hist, ai.NewUserMessage(userPromptParts...))

	// Inject static and dynamic information into the system prompt
	finalSystemPrompt := injectSystemPrompt(logger, l.systemPrompt, l.contextConfig)

	logger.Debug("message parts sending to LLM", slog.Any("parts", userPromptParts), slog.String("system_prompt", finalSystemPrompt))
	// TODO: We could re-use a flow here maybe, but for simplicity we create a new generate just for each message. We can optimize later if needed.

	toolLogger := logger
	genOpts := []ai.GenerateOption{
		ai.WithModel(l.model),
		ai.WithSystem(finalSystemPrompt),
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

func injectSystemPrompt(logger *slog.Logger, systemPrompt string, contextConfig config.ContextConfig) string {
	var contextInjector strings.Builder
	// Inject static data first
	for _, file := range contextConfig.StaticInject {
		content, err := os.ReadFile(file)
		if err != nil {
			logger.Warn("failed to load auto-inject file", slog.String("file", file), slog.String("error", err.Error()))
			continue
		}
		contextInjector.WriteString(fmt.Sprintf("\n\n[File: %s]\n%s", file, string(content)))
	}

	// Inject dynamic data
	for _, dd := range contextConfig.DynamicData {
		var cmd *exec.Cmd
		if len(dd.Args) > 0 {
			// Option A: Direct Execution (Safer, handles spaces in args perfectly)
			// Best for: date, weather-bin --city "New York"
			cmd = exec.Command(dd.Command, dd.Args...)
		} else {
			// Option B: Shell Magic (Allows pipes and redirects)
			// Best for: "ls | grep .go"
			cmd = exec.Command("sh", "-c", dd.Command)
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.Warn("failed to inject auto-inject", slog.String("name", dd.Name), slog.String("error", err.Error()))
			continue
		}
		contextInjector.WriteString(fmt.Sprintf("\n\n[%s] %s", dd.Name, strings.TrimSpace(string(out))))
	}
	contextInjector.WriteString("\n")

	return systemPrompt + contextInjector.String()
}
