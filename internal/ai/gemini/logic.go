// Package gemini contains the AI logic backed by Firebase Genkit (supports Gemini and other providers)
package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/genai"

	"hairy-botter/internal/ai/domain"
	"hairy-botter/internal/rag"
)

type historyLogic interface {
	Read(ctx context.Context, sessionID string) ([]*ai.Message, error)
	Save(ctx context.Context, sessionID string, history []*ai.Message) error
}

type contextKey string

const sessionIDKey contextKey = "x-session-id"

// Logic .
type Logic struct {
	logger *slog.Logger

	g       *genkit.Genkit
	model   ai.Model
	history historyLogic
	persona string

	// MCP tool refs stored for passing to Generate
	tools []ai.ToolRef

	// RAG related fields
	ragL *rag.Logic

	searchEnable bool
}

// New .
func New(logger *slog.Logger, g *genkit.Genkit, model ai.Model, history historyLogic, mcpClients []*client.Client, ragL *rag.Logic, searchEnable bool) (*Logic, error) {

	persona, err := readPersonality()
	if err != nil {
		return nil, err
	}

	tools := make([]ai.ToolRef, 0)
	if len(mcpClients) > 0 {
		logger.Info("MCP client list is not empty, initializing MCP clients")

		for idx, mcpClient := range mcpClients {
			initRequest := mcp.InitializeRequest{}
			initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
			initRequest.Params.ClientInfo = mcp.Implementation{
				Name:    "MCP-Go Simple Client Example",
				Version: "1.0.0",
			}

			initRequest.Params.Capabilities = mcp.ClientCapabilities{}

			ctx := context.Background()
			mcpServerInfo, err := mcpClient.Initialize(ctx, initRequest)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize mcp client: %w", err)
			}

			logger.Info("MCP server init success",
				slog.String("serverName", mcpServerInfo.ServerInfo.Name),
				slog.String("serverVersion", mcpServerInfo.ServerInfo.Version),
				slog.Any("serverCapabilities", mcpServerInfo.Capabilities))

			if mcpServerInfo.Capabilities.Tools != nil {
				logger.Info("Fetching available tools...")
				toolsRequest := mcp.ListToolsRequest{}
				toolsResult, err := mcpClient.ListTools(ctx, toolsRequest)
				if err != nil {
					logger.Error("Failed to list tools", "error", err)
					return nil, err
				}

				logger.Info("Tools available", slog.Int("toolsCount", len(toolsResult.Tools)),
					slog.Any("tools", toolsResult.Tools))

				for _, t := range toolsResult.Tools {
					capturedIdx := idx
					capturedName := t.Name
					capturedClients := mcpClients

					// Build JSON schema from MCP tool input schema
					var schemaMap map[string]any
					if b, err := t.InputSchema.MarshalJSON(); err == nil {
						_ = json.Unmarshal(b, &schemaMap)
					}

					// Skip re-registration if tool already defined (e.g. multiple New() calls)
					if genkit.LookupTool(g, capturedName) != nil {
						logger.Info("MCP tool already registered, skipping", slog.String("tool", capturedName))
						tools = append(tools, genkit.LookupTool(g, capturedName))
						continue
					}

					tool := genkit.DefineTool(g, capturedName, t.Description,
						func(tc *ai.ToolContext, input any) (any, error) {
							inputMap, _ := input.(map[string]any)
							ctr := mcp.CallToolRequest{}
							ctr.Params.Name = capturedName
							ctr.Params.Arguments = inputMap

							callRes, err := capturedClients[capturedIdx].CallTool(tc, ctr)
							if err != nil {
								return nil, err
							}

							textOutputs := make([]string, 0)
							for _, content := range callRes.Content {
								if textContent, ok := content.(mcp.TextContent); ok {
									textOutputs = append(textOutputs, textContent.Text)
								}
							}

							return map[string]any{"output": strings.Join(textOutputs, " ")}, nil
						},
						ai.WithInputSchema(schemaMap),
					)

					tools = append(tools, tool)
				}
			}
		}
	}

	return &Logic{
		logger:       logger,
		g:            g,
		model:        model,
		history:      history,
		persona:      persona,
		tools:        tools,
		ragL:         ragL,
		searchEnable: searchEnable,
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

	// Propagate session ID so MCP tool closures can forward it as a header
	ctx = context.WithValue(ctx, sessionIDKey, sessionID)

	// Build user message parts
	promptParts := make([]*ai.Part, 0)
	userMessagePrefix := ""
	if l.ragL != nil {
		logger.Info("adding RAG context to history")
		ragContent, err := l.ragL.Query(ctx, req.Message, 3)
		if err != nil {
			logger.Error("failed to query RAG content", slog.String("error", err.Error()))
			return "", err
		}

		ragRes := make([]string, 0)
		for _, res := range ragContent {
			ragRes = append(ragRes, res.Content)
		}

		if len(ragRes) > 0 {
			logger.Info("RAG content found, adding to the request", slog.Int("num_results", len(ragRes)))
			ragRes = append([]string{"Context from the knowledge base:"}, ragRes...)
			ragRes = append(ragRes, "\n")
			promptParts = append(promptParts, ai.NewTextPart(strings.Join(ragRes, "\n")))
			userMessagePrefix = "User request: "
		}
	}

	text := fmt.Sprintf("%s%s", userMessagePrefix, req.Message)
	if text != "" {
		promptParts = append(promptParts, ai.NewTextPart(text))
	}
	for _, inlineData := range req.InlineData {
		dataURL := "data:" + inlineData.MimeType + ";base64," + base64.StdEncoding.EncodeToString(inlineData.Data)
		promptParts = append(promptParts, ai.NewMediaPart(inlineData.MimeType, dataURL))
	}

	userMsg := ai.NewUserMessage(promptParts...)

	messages := append(hist, userMsg)

	// Assemble generate options
	opts := []ai.GenerateOption{
		ai.WithModel(l.model),
		ai.WithMessages(messages...),
		ai.WithSystem(l.persona),
	}

	logger.Info("creating chat content", slog.Bool("searchEnabled", l.searchEnable), slog.Int("mcpToolsCount", len(l.tools)))

	if len(l.tools) > 0 {
		opts = append(opts, ai.WithTools(l.tools...))
	} else if l.searchEnable {
		// Google Search grounding is Gemini-specific; passed via raw genai config
		opts = append(opts, ai.WithConfig(&genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{GoogleSearch: &genai.GoogleSearch{}},
			},
		}))
	}

	logger.Info("sending message")
	logger.Debug("message parts sending to model", slog.Any("parts", promptParts))

	resp, err := genkit.Generate(ctx, l.g, opts...)
	if err != nil {
		return "", err
	}

	// resp.History() returns request messages + final model message (genkit handles tool loop internally)
	err = l.history.Save(ctx, sessionID, resp.History())

	return resp.Text(), err
}

func readPersonality() (string, error) {
	b, err := os.ReadFile("personality.json")
	if err != nil {
		return "", err
	}

	// Support both plain string and genai.Content-style {"parts":[{"text":"..."}]} format
	var raw struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", err
	}

	if raw.Text != "" {
		return raw.Text, nil
	}

	texts := make([]string, 0, len(raw.Parts))
	for _, p := range raw.Parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}

	return strings.Join(texts, "\n"), nil
}
