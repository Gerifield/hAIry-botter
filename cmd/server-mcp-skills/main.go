package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	var port string
	var baseDir string
	var disableListFiles bool
	var disableReadFile bool
	var disableWriteFile bool
	var disableExecuteCommand bool

	flag.StringVar(&port, "port", "", "Port to listen on (default 8081 or PORT env var)")
	flag.StringVar(&baseDir, "base-dir", "", "Base directory for file operations (default . or BASE_DIR env var)")
	flag.BoolVar(&disableListFiles, "disable-list-files", false, "Disable the list_files tool (or DISABLE_LIST_FILES env var)")
	flag.BoolVar(&disableReadFile, "disable-read-file", false, "Disable the read_file tool (or DISABLE_READ_FILE env var)")
	flag.BoolVar(&disableWriteFile, "disable-write-file", false, "Disable the write_file tool (or DISABLE_WRITE_FILE env var)")
	flag.BoolVar(&disableExecuteCommand, "disable-execute-command", false, "Disable the execute_command tool (or DISABLE_EXECUTE_COMMAND env var)")
	flag.Parse()

	if port == "" {
		port = os.Getenv("PORT")
		if port == "" {
			port = "8081"
		}
	}
	if baseDir == "" {
		baseDir = os.Getenv("BASE_DIR")
		if baseDir == "" {
			baseDir = "."
		}
	}

	if !disableListFiles {
		disableListFiles = os.Getenv("DISABLE_LIST_FILES") == "true" || os.Getenv("DISABLE_LIST_FILES") == "1"
	}
	if !disableReadFile {
		disableReadFile = os.Getenv("DISABLE_READ_FILE") == "true" || os.Getenv("DISABLE_READ_FILE") == "1"
	}
	if !disableWriteFile {
		disableWriteFile = os.Getenv("DISABLE_WRITE_FILE") == "true" || os.Getenv("DISABLE_WRITE_FILE") == "1"
	}
	if !disableExecuteCommand {
		disableExecuteCommand = os.Getenv("DISABLE_EXECUTE_COMMAND") == "true" || os.Getenv("DISABLE_EXECUTE_COMMAND") == "1"
	}

	srv := server.NewMCPServer("Skills Server", "0.0.1")

	// Register List Files Tool
	if !disableListFiles {
		srv.AddTool(mcp.NewTool("list_files",
			mcp.WithDescription("List files and directories in a given path relative to the base directory."),
			mcp.WithString("path", mcp.Description("The directory path to list files from (default: current directory).")),
		), handleListFiles(baseDir))
	}

	// Register Read File Tool
	if !disableReadFile {
		srv.AddTool(mcp.NewTool("read_file",
			mcp.WithDescription("Read the contents of a file relative to the base directory."),
			mcp.WithString("path", mcp.Required(), mcp.Description("The file path to read.")),
		), handleReadFile(baseDir))
	}

	// Register Write File Tool
	if !disableWriteFile {
		srv.AddTool(mcp.NewTool("write_file",
			mcp.WithDescription("Write content to a file relative to the base directory, creating directories as needed and overwriting existing files."),
			mcp.WithString("path", mcp.Required(), mcp.Description("The file path to write to.")),
			mcp.WithString("content", mcp.Required(), mcp.Description("The content to write.")),
		), handleWriteFile(baseDir))
	}

	// Register Execute Command Tool
	if !disableExecuteCommand {
		srv.AddTool(mcp.NewTool("execute_command",
			mcp.WithDescription("Execute a shell command in the base directory and get the output. Supports pipes and redirections via sh -c."),
			mcp.WithString("command", mcp.Required(), mcp.Description("The shell command to execute.")),
		), handleExecuteCommand(baseDir))
	}

	// Setup Streamable HTTP Server
	streamableSrv := server.NewStreamableHTTPServer(srv,
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			return context.WithValue(ctx, "x-session-id", r.Header.Get("x-session-id"))
		}))

	slog.Info("starting Skills MCP Server",
		slog.String("port", port),
		slog.String("base-dir", baseDir),
	)

	if err := streamableSrv.Start(":" + port); err != nil {
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error(err.Error())
		}
	}
}
