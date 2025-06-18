// Package gemini contains the Gemini implementation of the AI logic
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/genai"

	"hairy-botter/internal/rag"
)

type historyLogic interface {
	Read(ctx context.Context, sessionID string) ([]*genai.Content, error)
	Save(ctx context.Context, sessionID string, history []*genai.Content) error
}

// Logic .
type Logic struct {
	logger *slog.Logger

	client  *genai.Client
	model   string
	history historyLogic
	persona *genai.Content

	// MCP related fields
	// We have multiple clients in a list
	mcpClients []*client.Client
	// We store all the function in the same list -> we need a mapping for the clients
	mcpFunctions []*genai.FunctionDeclaration
	// map function name to client index -> Note: this prevents to reuse the same function name!!
	mcpFunctionMap map[string]int

	// RAG related fields
	ragL *rag.Logic

	searchEnable bool
}

// New .
func New(logger *slog.Logger, client *genai.Client, model string, history historyLogic, mcpClients []*client.Client, ragL *rag.Logic, searchEnable bool) (*Logic, error) {

	persona, err := readPersonality()
	if err != nil {
		return nil, err
	}

	fnMapping := make(map[string]int)
	functions := make([]*genai.FunctionDeclaration, 0)
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
				} else {
					logger.Info("Tools available", slog.Int("toolsCount", len(toolsResult.Tools)),
						slog.Any("tools", toolsResult.Tools))
				}

				for _, t := range toolsResult.Tools {
					// Conversion for schema
					b, _ := t.InputSchema.MarshalJSON()
					convSchema := &genai.Schema{}
					schemaErr := convSchema.UnmarshalJSON(b)
					if schemaErr != nil {
						slog.Error("Failed to unmarshal parameter schema", "error", schemaErr)

						continue
					}

					functions = append(functions, &genai.FunctionDeclaration{
						Name:        t.Name,
						Description: t.Description,
						Parameters:  convSchema,
					})

					// Add to the mapping fn name -> client index
					fnMapping[t.Name] = idx
				}
			}
		}
	}

	return &Logic{
		logger:         logger,
		client:         client,
		model:          model,
		history:        history,
		persona:        persona,
		mcpClients:     mcpClients,
		mcpFunctions:   functions,
		mcpFunctionMap: fnMapping,
		ragL:           ragL,
		searchEnable:   searchEnable,
	}, nil
}

// HandleMessage as an internal logic
// sessionID is unique to be able to get the history
func (l *Logic) HandleMessage(ctx context.Context, sessionID string, msg string) (string, error) {
	if sessionID == "" {
		return "", errors.New("sessionID is empty")
	}
	logger := l.logger.With("sessionID", sessionID)
	logger.Info("handling message", slog.String("message", msg))

	hist, err := l.history.Read(ctx, sessionID)
	if err != nil {
		return "", err
	}

	zeroBudget := int32(0)
	createConfig := &genai.GenerateContentConfig{
		SystemInstruction: l.persona,
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingBudget: &zeroBudget,
		},
	}

	// Add MCP tools if available
	if len(l.mcpClients) > 0 {
		createConfig.Tools = []*genai.Tool{
			{FunctionDeclarations: l.mcpFunctions},
		}
	} else if l.searchEnable {
		createConfig.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
	}

	logger.Info("creating chat content", slog.Bool("searchEnabled", l.searchEnable), slog.Int("mcpClientsCount", len(l.mcpClients)))
	ch, err := l.client.Chats.Create(ctx, l.model, createConfig, hist)
	if err != nil {
		return "", err
	}

	// Add RAG information if available to the user prompt as context
	promptParts := make([]genai.Part, 0)
	if l.ragL != nil {
		logger.Info("adding RAG context to history")
		ragContent, err := l.ragL.Query(ctx, msg, 3) // Query with the message as context
		if err != nil {
			logger.Error("failed to query RAG content", slog.String("error", err.Error()))

			return "", err
		}

		// Collect the results into a string slice
		ragRes := make([]string, 0)
		for _, res := range ragContent {
			ragRes = append(ragRes, res.Content)
		}

		// Convert the result to a single genai.Part
		if len(ragRes) > 0 {
			logger.Info("RAG content found, adding to the request", slog.Int("num_results", len(ragRes)))

			// Add a little context info and an additional line break
			ragRes = append([]string{"Context from the knowledge base:"}, ragRes...)
			ragRes = append(ragRes, "\n")

			promptParts = append(promptParts, genai.Part{
				Text: strings.Join(ragRes, "\n"),
			})
		}
	}

	logger.Info("sending message")
	parts := append(promptParts, genai.Part{Text: fmt.Sprintf("User request: %s", msg)})
	logger.Debug("message parts sending to Gemini", slog.Any("parts", parts))
	resp, err := ch.SendMessage(ctx, parts...)
	if err != nil {
		return "", err
	}

	// Check and handle function calls
	resp, err = l.resolveFunctions(ctx, sessionID, logger, ch, resp)
	if err != nil {
		logger.Error("failed to resolve functions", slog.String("error", err.Error()))

		return "", err
	}

	err = l.history.Save(ctx, sessionID, ch.History(false))

	return resp.Text(), err
}

func (l *Logic) resolveFunctions(ctx context.Context, sessionID string, logger *slog.Logger, ch *genai.Chat, resp *genai.GenerateContentResponse) (*genai.GenerateContentResponse, error) {
	for {
		calls := resp.FunctionCalls()
		if len(calls) == 0 {
			break
		}

		logger.Info("function calls detected", slog.Int("calls", len(calls)))
		msgParts := make([]genai.Part, 0)
		for _, call := range calls {
			// fmt.Println(call.ID, call.Name, call.Args)
			logger.Info("initiating function call", slog.String("id", call.ID), slog.String("function", call.Name), slog.Any("args", call.Args))

			clientIdx, ok := l.mcpFunctionMap[call.Name]
			if !ok {
				logger.Error("function call not found in MCP function list", slog.String("function", call.Name))

				continue
			}
			ctr := mcp.CallToolRequest{}
			ctr.Params.Name = call.Name
			ctr.Params.Arguments = call.Args
			ctx = context.WithValue(ctx, "x-session-id", sessionID)
			callRes, err := l.mcpClients[clientIdx].CallTool(ctx, ctr)
			if err != nil {
				logger.Error("Failed to call tool", "error", err)

				continue
			}

			// TODO: Add more than text support?
			textOutputs := make([]string, 0)
			for _, content := range callRes.Content {
				if textContent, ok := content.(mcp.TextContent); ok {
					textOutputs = append(textOutputs, textContent.Text)
				}
			}

			textOutput := strings.Join(textOutputs, " ")
			logger.Info("function call result", slog.String("id", call.ID), slog.String("function", call.Name), slog.String("output", textOutput))

			msgParts = append(msgParts,
				genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						ID:   call.ID, // Opt. only
						Name: call.Name,
						Response: map[string]any{
							"output": textOutput,
							// "error"
						},
					}})
		}

		// Resend with the function output
		var err error
		resp, err = ch.SendMessage(ctx, msgParts...)
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func readPersonality() (*genai.Content, error) {
	b, err := os.ReadFile("personality.json")
	if err != nil {
		return nil, err
	}

	var c genai.Content
	err = json.Unmarshal(b, &c)
	if err != nil {
		return nil, err
	}

	return &c, nil
}
