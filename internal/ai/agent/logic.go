// Package agent contains the AI logic backed by Firebase Genkit (supports Gemini and other providers)
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"hairy-botter/internal/ai/domain"
	"hairy-botter/internal/rag"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	genkitMCP "github.com/firebase/genkit/go/plugins/mcp"
)

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

	toolRefs     []ai.ToolRef
	customConfig any

	// RAG related fields
	ragL *rag.Logic
}

// New .
func New(logger *slog.Logger, g *genkit.Genkit, model ai.Model, history historyLogic, mcpClientAddrs []string, ragL *rag.Logic, customConfig any) (*Logic, error) {
	var tools []ai.Tool
	persona, err := readPersonality()
	if err != nil {
		return nil, err
	}

	if len(mcpClientAddrs) > 0 {
		mcpServers := make([]genkitMCP.MCPServerConfig, 0, len(mcpClientAddrs))
		for i, addr := range mcpClientAddrs {
			mcpServers = append(mcpServers, genkitMCP.MCPServerConfig{
				Name: fmt.Sprintf("mcp-client-%d", i), // Unique name for each client
				Config: genkitMCP.MCPClientOptions{
					StreamableHTTP: &genkitMCP.StreamableHTTPConfig{BaseURL: addr},
				},
			})
		}

		logger.Info("MCP client list is not empty, initializing MCP clients")
		mcpManager, err := genkitMCP.NewMCPHost(g, genkitMCP.MCPHostOptions{
			Name:       "hairy-botter-mcp-host",
			Version:    "1.0.0",
			MCPServers: mcpServers,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize MCP host: %w", err)
		}

		tools, err = mcpManager.GetActiveTools(context.Background(), g)
		if err != nil {
			return nil, fmt.Errorf("failed to get active tools from MCP host: %w", err)
		}
	}

	// Convert the ai.Tools to ai.ToolRefs
	toolRefs := make([]ai.ToolRef, len(tools))
	for i, tool := range tools {
		toolRefs[i] = tool
	}

	return &Logic{
		logger:       logger,
		g:            g,
		model:        model,
		history:      history,
		persona:      persona,
		toolRefs:     toolRefs,
		customConfig: customConfig,
		ragL:         ragL,
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

	userPromptParts := make([]*ai.Part, 0)
	for _, inlineData := range req.InlineData {
		// If we have some inline data convert them to prompt parts
		userPromptParts = append(userPromptParts, ai.NewMediaPart(inlineData.MimeType, string(inlineData.Data)))
	}

	// Add the user's request at the end too
	userPromptParts = append(userPromptParts, ai.NewTextPart(req.Message))
	hist = append(hist, ai.NewUserMessage(userPromptParts...))

	logger.Debug("message parts sending to LLM", slog.Any("parts", userPromptParts))
	// TODO: We could re-use a flow here maybe, but for simplicity we create a new generate just for each message. We can optimize later if needed.

	genOpts := []ai.GenerateOption{
		ai.WithModel(l.model),
		ai.WithSystem(l.persona),
		ai.WithTools(l.toolRefs...),
		ai.WithToolChoice(ai.ToolChoiceAuto),
		ai.WithMessages(hist...),
		ai.WithConfig(l.customConfig), // It has a nil check internally
	}

	if len(ragContextDocs) > 0 {
		genOpts = append(genOpts, ai.WithDocs(ragContextDocs...))
	}

	resp, err := genkit.Generate(ctx, l.g, genOpts...) // TODO: if we rewrite, make this smarter
	if err != nil {
		return "", err
	}

	// TODO: Think about a better history management, since this contains the RAG messages too, maybe we want to separate them? For now we just save everything in the history, but we could optimize later if needed.
	err = l.history.Save(ctx, sessionID, resp.History())

	return resp.Text(), err
}

func readPersonality() (string, error) {
	b, err := os.ReadFile("personality.txt")
	if err != nil {
		return "", err
	}

	return string(b), nil
}
