// Package mcpserver exposes an agent.Logic as an MCP server so it can be called
// as a sub-agent by an orchestrator that discovers tools via the MCP protocol.
package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"hairy-botter/internal/ai/domain"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type agentHandler interface {
	HandleMessage(ctx context.Context, sessionID string, req domain.Request) (string, error)
}

// Config holds the metadata this agent advertises to orchestrators.
// Orchestrators read tool descriptions at discovery time to decide routing,
// so embedding role and tool names there avoids a separate info round-trip.
type Config struct {
	// Name is the MCP server name sent during capability negotiation.
	Name string
	// Description is a short, human-readable summary of this agent's role or
	// specialty (e.g. "Research assistant with web search and file access").
	// Shown in the chat tool description so orchestrators can route without
	// calling info first.
	Description string
	// ToolNames lists every tool this agent has access to.
	// Included verbatim in the chat tool description.
	ToolNames []string
}

// Server wraps an agent as an MCP sub-agent server.
type Server struct {
	logic  agentHandler
	cfg    Config
	mcpSrv *server.MCPServer
}

// New creates a Server that exposes two MCP tools:
//
//   - chat(message, session_id?) — send a message and get a response.
//     The tool description embeds the agent's role and its available tools
//     so orchestrators can make routing decisions without an extra info call.
//
//   - info() — returns the full Config details as plain text.
func New(logic agentHandler, cfg Config) *Server {
	if cfg.Name == "" {
		cfg.Name = "hairy-botter-agent"
	}

	mcpSrv := server.NewMCPServer(cfg.Name, "1.0.0")
	s := &Server{logic: logic, cfg: cfg, mcpSrv: mcpSrv}

	toolList := "none"
	if len(cfg.ToolNames) > 0 {
		toolList = strings.Join(cfg.ToolNames, ", ")
	}

	mcpSrv.AddTool(
		mcp.NewTool("chat",
			mcp.WithDescription(fmt.Sprintf(
				"Send a message to this AI agent and receive a response. "+
					"Agent role: %s. Tools this agent can use: [%s].",
				cfg.Description, toolList,
			)),
			mcp.WithString("message",
				mcp.Required(),
				mcp.Description("The message to send to the agent."),
			),
			mcp.WithString("session_id",
				mcp.Description("Conversation session ID for multi-turn continuity. "+
					"Omit or leave empty to start a fresh session."),
			),
		),
		s.handleChat,
	)

	mcpSrv.AddTool(
		mcp.NewTool("info",
			mcp.WithDescription(
				"Returns this agent's name, role description, and the full list of tools it has access to.",
			),
		),
		s.handleInfo,
	)

	return s
}

// Start runs the MCP server on addr (e.g. ":8082") using the Streamable HTTP transport.
func (s *Server) Start(addr string) error {
	streamSrv := server.NewStreamableHTTPServer(s.mcpSrv,
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			if sid := r.Header.Get("x-session-id"); sid != "" {
				return context.WithValue(ctx, "x-session-id", sid)
			}
			return ctx
		}),
	)
	return streamSrv.Start(addr)
}

func (s *Server) handleChat(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	msg := req.GetString("message", "")
	if msg == "" {
		return mcp.NewToolResultError("message is required"), nil
	}
	sessionID := req.GetString("session_id", "")
	if sessionID == "" {
		sessionID = "mcp-default"
	}

	resp, err := s.logic.HandleMessage(ctx, sessionID, domain.Request{Message: msg})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("agent error: %v", err)), nil
	}
	return mcp.NewToolResultText(resp), nil
}

func (s *Server) handleInfo(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	toolList := "none"
	if len(s.cfg.ToolNames) > 0 {
		toolList = strings.Join(s.cfg.ToolNames, ", ")
	}
	text := fmt.Sprintf("Name: %s\nRole: %s\nAvailable tools: %s",
		s.cfg.Name, s.cfg.Description, toolList)
	return mcp.NewToolResultText(text), nil
}
