package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {

	srv := server.NewMCPServer(
		"Greeter example",
		"0.0.1")

	toolGreeter := mcp.NewTool("some_random_function",
		mcp.WithDescription("This is the some_random_function which the user can call with or without a name parameter and it will do something"),
		mcp.WithString("name",
			mcp.Description("Name of the random action to be taken"),
			// mcp.DefaultString("Anonymous"),
		),
	)

	srv.AddTool(toolGreeter, handleSomeRandomFunction)

	sseSrv := server.NewSSEServer(srv,
		server.WithBaseURL("http://localhost:8081"),
		server.WithSSEContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, "x-session-id", r.Header.Get("x-session-id"))
		}))
	slog.Info("starting SSE server", slog.String("url", "http://localhost:8081"))
	if err := sseSrv.Start(":8081"); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error(err.Error())
		}
	}
}

func handleSomeRandomFunction(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sid, ok := ctx.Value("x-session-id").(string)
	if !ok {
		return nil, errors.New("session id not found in context")
	}
	slog.Info("handling SomeRandomFunction", slog.Any("request", request), slog.String("sid", sid))
	name, err := request.RequireString("name")
	if err != nil {
		return nil, err
	}

	if name == "" {
		return mcp.NewToolResultText("Hello mysterious stranger! Randomly"), nil
	}

	return mcp.NewToolResultText("Hello " + name + ", welcome here I will do something maybe!"), nil
}
