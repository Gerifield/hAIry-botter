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

	"hairy-botter/internal/rag"
	
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/genai"
)

// Logic .
type Logic struct {
	logger *slog.Logger

	client      *genai.Client
	model       string
	historyPath string
	persona     *genai.Content

	// MCP related fields
	// We have multiple clients in a list
	mcpClients []*client.Client
	// We store all the function in the same list -> we need a mapping for the clients
	mcpFunctions []*genai.FunctionDeclaration
	// map function name to client index -> Note: this prevents to reuse the same function name!!
	mcpFunctionMap map[string]int

	// RAG related fields
	ragL *rag.Logic
}

// New .
func New(logger *slog.Logger, client *genai.Client, model string, historyPath string, mcpClients []*client.Client, ragL *rag.Logic) (*Logic, error) {

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
		historyPath:    historyPath,
		persona:        persona,
		mcpClients:     mcpClients,
		mcpFunctions:   functions,
		mcpFunctionMap: fnMapping,
		ragL:           ragL,
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

	hist, err := l.readHistory(sessionID)
	if err != nil {
		return "", err
	}

	createConfig := &genai.GenerateContentConfig{
		SystemInstruction: l.persona,
	}

	// Add MCP tools if available
	if len(l.mcpClients) > 0 {
		createConfig.Tools = []*genai.Tool{
			{FunctionDeclarations: l.mcpFunctions},
		}
	}

	logger.Info("creating chat content")
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

	err = l.saveHistory(sessionID, ch.History(false))

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

type saveFormat struct {
	History []*genai.Content `json:"history"`
}

func (l *Logic) readHistory(sessionID string) ([]*genai.Content, error) {
	b, err := os.ReadFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) { // Not yet exists, ignore
			return make([]*genai.Content, 0), nil
		}
		return nil, err
	}

	var saved saveFormat
	err = json.Unmarshal(b, &saved)
	if err != nil {
		return nil, err
	}

	return saved.History, nil
}

func (l *Logic) saveHistory(sessionID string, history []*genai.Content) error {
	b, err := json.Marshal(saveFormat{
		History: history,
	})
	if err != nil {
		return err
	}

	return os.WriteFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID), b, 0644)
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
