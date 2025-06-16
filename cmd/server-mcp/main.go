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

	streamableSrv := server.NewStreamableHTTPServer(srv,
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, "x-session-id", r.Header.Get("x-session-id"))
		}))
	slog.Info("starting Streamable HTTP server", slog.String("url", "http://localhost:8081/mcp"))
	if err := streamableSrv.Start(":8081"); err != nil {
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
	name := request.GetString("name", "")

	if name == "" {
		return mcp.NewToolResultText("Hello mysterious stranger! Randomly"), nil
	}

	return mcp.NewToolResultText("Hello " + name + ", welcome here I will do something maybe!"), nil
}
