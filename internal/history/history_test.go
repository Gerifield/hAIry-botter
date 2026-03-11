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
	// sessionID = "../secret.txt"
	// filepath.Base("../secret.txt") -> "secret.txt"
	// filepath.Join(historyDir, "secret.txt") -> tmpDir + "/history/secret.txt" (should not exist)
	_, err = l.Read(ctx, "../secret.txt")
	if err != nil && err.Error() == "invalid character 't' looking for beginning of value" {
		t.Error("Vulnerability still exists: Read() accessed file outside of history directory")
	}

	// Verify it looked in the right place (or at least not the wrong place)
	expectedPath := filepath.Join(historyDir, "secret.txt")
	if _, err := os.Stat(expectedPath); err == nil {
		t.Errorf("Unexpectedly found file at %s", expectedPath)
	}

	// Test Save with path traversal attempt
	err = l.Save(ctx, "../attack.txt", nil)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// attack.txt should be in historyDir, NOT in tmpDir
	if _, err := os.Stat(filepath.Join(tmpDir, "attack.txt")); err == nil {
		t.Error("Vulnerability still exists: Save() wrote file outside of history directory")
	}

	if _, err := os.Stat(filepath.Join(historyDir, "attack.txt")); os.IsNotExist(err) {
		t.Error("Save() did not write file to history directory")
	}
}
