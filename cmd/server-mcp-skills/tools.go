package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// ensureSafePath checks if the given path is within the base directory.
// It returns the absolute safe path or an error if the path traverses outside the base directory.
func ensureSafePath(baseDir, reqPath string) (string, error) {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute base directory: %w", err)
	}

	targetPath := filepath.Join(absBase, reqPath)
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute target path: %w", err)
	}

	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}

	// filepath.Rel returns a path starting with ".." if the target is outside the base directory.
	// It returns "." if target is the same as base directory.
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "..\\") {
		return "", fmt.Errorf("path traversal attempt detected: %s", reqPath)
	}

	return absTarget, nil
}

// handleListFiles creates the handler for listing files recursively.
func handleListFiles(baseDir string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pathArg := req.GetString("path", ".")

		safePath, err := ensureSafePath(baseDir, pathArg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		var files []string
		err = filepath.WalkDir(safePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(safePath, path)
			if err != nil {
				return nil // Skip if we can't get relative path
			}
			if relPath == "." {
				return nil
			}

			if d.IsDir() {
				files = append(files, relPath+"/")
			} else {
				files = append(files, relPath)
			}
			return nil
		})

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list files: %v", err)), nil
		}

		if len(files) == 0 {
			return mcp.NewToolResultText("Directory is empty"), nil
		}

		return mcp.NewToolResultText(strings.Join(files, "\n")), nil
	}
}

// handleReadFile creates the handler for reading a file's contents.
func handleReadFile(baseDir string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pathArg := req.GetString("path", "")
		if pathArg == "" {
			return mcp.NewToolResultError("path parameter is required"), nil
		}

		safePath, err := ensureSafePath(baseDir, pathArg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		content, err := os.ReadFile(safePath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read file: %v", err)), nil
		}

		return mcp.NewToolResultText(string(content)), nil
	}
}

// handleWriteFile creates the handler for writing content to a file.
func handleWriteFile(baseDir string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pathArg := req.GetString("path", "")
		if pathArg == "" {
			return mcp.NewToolResultError("path parameter is required"), nil
		}

		contentArg := req.GetString("content", "")

		safePath, err := ensureSafePath(baseDir, pathArg)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid path: %v", err)), nil
		}

		// Ensure the directory exists before writing the file
		dir := filepath.Dir(safePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create directories: %v", err)), nil
		}

		err = os.WriteFile(safePath, []byte(contentArg), 0644)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to write file: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(contentArg), pathArg)), nil
	}
}

// handleExecuteCommand creates the handler for executing a shell command.
func handleExecuteCommand(baseDir string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cmdArg := req.GetString("command", "")
		if cmdArg == "" {
			return mcp.NewToolResultError("command parameter is required"), nil
		}

		absBase, err := filepath.Abs(baseDir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get absolute base directory: %v", err)), nil
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdArg)
		cmd.Dir = absBase

		output, err := cmd.CombinedOutput()
		if err != nil {
			// Include both the output and the error message
			errorMsg := fmt.Sprintf("Command failed with error: %v\nOutput:\n%s", err, string(output))
			return mcp.NewToolResultError(errorMsg), nil
		}

		return mcp.NewToolResultText(string(output)), nil
	}
}
