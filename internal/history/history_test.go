package history

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestPathTraversalFixed(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	historyDir := filepath.Join(tmpDir, "history")
	err = os.Mkdir(historyDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	secretFile := filepath.Join(tmpDir, "secret.txt")
	secretContent := "top secret"
	err = os.WriteFile(secretFile, []byte(secretContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	l := New(logger, historyDir, Config{})

	ctx := context.Background()

	// Test Read with path traversal attempt
	_, err = l.Read(ctx, "../secret.txt")
	if err == nil || err.Error() != "invalid sessionID" {
		t.Errorf("Expected 'invalid sessionID' error, got: %v", err)
	}

	// Test Save with path traversal attempt
	err = l.Save(ctx, "../attack.txt", nil)
	if err == nil || err.Error() != "invalid sessionID" {
		t.Errorf("Expected 'invalid sessionID' error, got: %v", err)
	}

	// attack.txt should NOT be written anywhere
	if _, err := os.Stat(filepath.Join(tmpDir, "attack.txt")); err == nil {
		t.Error("Vulnerability still exists: Save() wrote file outside of history directory")
	}

	if _, err := os.Stat(filepath.Join(historyDir, "attack.txt")); err == nil {
		t.Error("Save() unexpectedly wrote file to history directory")
	}
}

func TestSaveCreatesDirectoryIfMissing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history-mkdir-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	missingDir := filepath.Join(tmpDir, "nested", "history-dir")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	l := New(logger, missingDir, Config{})

	ctx := context.Background()
	if err := l.Save(ctx, "session-123", nil); err != nil {
		t.Fatalf("Save() failed when directory did not exist: %v", err)
	}

	if _, err := os.Stat(filepath.Join(missingDir, "session-123")); err != nil {
		t.Errorf("expected session file to be created, got error: %v", err)
	}
}
