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
	if evalBase, err := filepath.EvalSymlinks(absBase); err == nil {
		absBase = evalBase
	}

	// Create an outside directory and file to test symlink escapes
	outsideDir, err := os.MkdirTemp("", "mcp-skills-outside-*")
	if err != nil {
		t.Fatalf("failed to create outside temp dir: %v", err)
	}
	defer os.RemoveAll(outsideDir)

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}

	// Create symlinks inside absBase pointing outside
	symlinkFile := filepath.Join(absBase, "escape_file.txt")
	_ = os.Symlink(outsideFile, symlinkFile)

	symlinkDir := filepath.Join(absBase, "escape_dir")
	_ = os.Symlink(outsideDir, symlinkDir)

	// Create a valid internal file and symlink inside absBase
	insideFile := filepath.Join(absBase, "real.txt")
	if err := os.WriteFile(insideFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write inside file: %v", err)
	}
	validSymlink := filepath.Join(absBase, "valid_link.txt")
	_ = os.Symlink(insideFile, validSymlink)

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
		{
			name:      "valid internal symlink",
			reqPath:   "valid_link.txt",
			expectErr: false,
		},
		{
			name:      "symlink traversal - file pointing outside",
			reqPath:   "escape_file.txt",
			expectErr: true,
			contains:  "path traversal attempt detected",
		},
		{
			name:      "symlink traversal - dir pointing outside with new file",
			reqPath:   "escape_dir/new_outside.txt",
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
