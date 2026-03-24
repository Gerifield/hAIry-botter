package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureSafePath(t *testing.T) {
	// Create a temporary base directory for testing
	baseDir, err := os.MkdirTemp("", "mcp-skills-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(baseDir)

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		t.Fatalf("failed to get absolute base dir: %v", err)
	}

	tests := []struct {
		name      string
		reqPath   string
		expectErr bool
		contains  string // error message should contain this
	}{
		{
			name:      "simple relative path",
			reqPath:   "test.txt",
			expectErr: false,
		},
		{
			name:      "nested directory path",
			reqPath:   "subdir/test.txt",
			expectErr: false,
		},
		{
			name:      "path is dot",
			reqPath:   ".",
			expectErr: false,
		},
		{
			name:      "empty path",
			reqPath:   "",
			expectErr: false,
		},
		{
			name:      "path traversal - one level up",
			reqPath:   "../outside.txt",
			expectErr: true,
			contains:  "path traversal attempt detected",
		},
		{
			name:      "path traversal - multiple levels up",
			reqPath:   "../../../../etc/passwd",
			expectErr: true,
			contains:  "path traversal attempt detected",
		},
		{
			name:      "path traversal - sneaky dot dot",
			reqPath:   "subdir/../../outside.txt",
			expectErr: true,
			contains:  "path traversal attempt detected",
		},
	}

	// OS-specific tests
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name      string
			reqPath   string
			expectErr bool
			contains  string
		}{
			name:      "Windows absolute path",
			reqPath:   `C:\Windows\System32\drivers\etc\hosts`,
			expectErr: true,
			contains:  "path traversal attempt detected",
		})
	} else {
		tests = append(tests, struct {
			name      string
			reqPath   string
			expectErr bool
			contains  string
		}{
			name:      "Unix absolute path",
			reqPath:   "/etc/passwd",
			expectErr: true,
			contains:  "path traversal attempt detected",
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ensureSafePath(absBase, tt.reqPath)
			if tt.expectErr {
				if err == nil {
					t.Errorf("ensureSafePath() expected error, got nil")
					return
				}
				if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
					t.Errorf("ensureSafePath() error = %v, want error containing %v", err, tt.contains)
				}
			} else {
				if err != nil {
					t.Errorf("ensureSafePath() unexpected error: %v", err)
					return
				}
				// Verify the returned path is absolute and starts with absBase
				if !filepath.IsAbs(got) {
					t.Errorf("ensureSafePath() returned non-absolute path: %s", got)
				}
				rel, err := filepath.Rel(absBase, got)
				if err != nil {
					t.Errorf("failed to get relative path from result: %v", err)
				}
				if strings.HasPrefix(rel, "..") {
					t.Errorf("ensureSafePath() returned path outside base: %s", got)
				}
			}
		})
	}
}
