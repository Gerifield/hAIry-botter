package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// To test this binary we can compile it and run the resulting binary.
func buildBinary(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "cachefor")

	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(out))
	}

	return binPath
}

func TestCacheFor(t *testing.T) {
	binPath := buildBinary(t)
	tempDir := t.TempDir()

	// Switch working directory to tempDir so cache files are created there
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}

	// Helper to generate hash
	getHash := func(cmdArgs ...string) string {
		cmdStr := strings.Join(cmdArgs, " ")
		h := sha1.New()
		h.Write([]byte(cmdStr))
		return fmt.Sprintf("%x", h.Sum(nil))
	}

	t.Run("basic command cache miss and hit", func(t *testing.T) {
		args := []string{"echo", "hello test"}

		// First run - cache miss
		cmd := exec.Command(binPath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("First run failed: %v", err)
		}
		if !strings.Contains(string(out), "hello test") {
			t.Errorf("Expected 'hello test' in output, got: %s", string(out))
		}

		// Check cache files
		hash := getHash(args...)
		cacheFile := fmt.Sprintf("%s.cache", hash)
		if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
			t.Errorf("Cache file %s was not created", cacheFile)
		}

		// Second run - cache hit
		cmd = exec.Command(binPath, args...)
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Second run failed: %v", err)
		}
		if !strings.Contains(string(out), "hello test") {
			t.Errorf("Expected 'hello test' in output, got: %s", string(out))
		}
	})

	t.Run("error output and exit code", func(t *testing.T) {
		// create a temporary script
		scriptPath := filepath.Join(tempDir, "test.sh")
		scriptContent := `#!/bin/sh
echo "standard out"
echo "standard err" >&2
exit 42
`
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
			t.Fatalf("Failed to write test script: %v", err)
		}

		args := []string{scriptPath}

		// First run - cache miss
		cmd := exec.Command(binPath, args...)
		out, err := cmd.CombinedOutput()

		// Expecting error because exit code is 42
		if err == nil {
			t.Fatal("Expected command to fail")
		}

		var exitCode int
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Expected ExitError, got %v", err)
		}

		if exitCode != 42 {
			t.Errorf("Expected exit code 42, got %d", exitCode)
		}

		if !strings.Contains(string(out), "standard out") || !strings.Contains(string(out), "standard err") {
			t.Errorf("Expected both stdout and stderr in output, got: %s", string(out))
		}

		// Second run - cache hit
		cmd = exec.Command(binPath, args...)
		out, err = cmd.CombinedOutput()

		if err == nil {
			t.Fatal("Expected command to fail on cache hit too")
		}

		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			t.Fatalf("Expected ExitError on cache hit, got %v", err)
		}

		if exitCode != 42 {
			t.Errorf("Expected exit code 42 on cache hit, got %d", exitCode)
		}

		if !strings.Contains(string(out), "standard out") || !strings.Contains(string(out), "standard err") {
			t.Errorf("Expected both stdout and stderr in output from cache hit, got: %s", string(out))
		}
	})

	t.Run("cache expiration", func(t *testing.T) {
		args := []string{"echo", "expire test"}
		hash := getHash(args...)
		cacheFile := fmt.Sprintf("%s.cache", hash)

		// Set cache time to 1s via env
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(), "CACHE_TIME=1s")

		if err := cmd.Run(); err != nil {
			t.Fatalf("First run failed: %v", err)
		}

		// Manually backdate the cache file by 2 seconds
		pastTime := time.Now().Add(-2 * time.Second)
		if err := os.Chtimes(cacheFile, pastTime, pastTime); err != nil {
			t.Fatalf("Failed to backdate cache file: %v", err)
		}

		// The script writes a new output, but since we are running "echo", it will be identical.
		// However, the mod time should be updated if it's a miss.
		cmd = exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(), "CACHE_TIME=1s")

		if err := cmd.Run(); err != nil {
			t.Fatalf("Second run failed: %v", err)
		}

		stat, err := os.Stat(cacheFile)
		if err != nil {
			t.Fatalf("Failed to stat cache file: %v", err)
		}

		if stat.ModTime().Before(time.Now().Add(-1 * time.Second)) {
			t.Errorf("Cache file should have been updated, mod time is %v", stat.ModTime())
		}
	})

	t.Run("cleanup old caches", func(t *testing.T) {
		// Create a dummy old cache file
		oldCacheFile := "old_test.cache"
		if err := os.WriteFile(oldCacheFile, []byte("old"), 0644); err != nil {
			t.Fatalf("Failed to write old cache file: %v", err)
		}

		// Create a dummy recent cache file
		recentCacheFile := "recent_test.cache"
		if err := os.WriteFile(recentCacheFile, []byte("recent"), 0644); err != nil {
			t.Fatalf("Failed to write recent cache file: %v", err)
		}

		// Backdate old cache file to be older than 2 * cacheTime (cacheTime will be 1s)
		oldTime := time.Now().Add(-3 * time.Second)
		if err := os.Chtimes(oldCacheFile, oldTime, oldTime); err != nil {
			t.Fatalf("Failed to backdate old cache file: %v", err)
		}

		// Run the binary to trigger cleanup
		cmd := exec.Command(binPath, "echo", "cleanup test")
		cmd.Env = append(os.Environ(), "CACHE_TIME=1s")

		if err := cmd.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		// Verify old cache was deleted
		if _, err := os.Stat(oldCacheFile); !os.IsNotExist(err) {
			t.Errorf("Old cache file %s should have been deleted", oldCacheFile)
		}

		// Verify recent cache was kept
		if _, err := os.Stat(recentCacheFile); os.IsNotExist(err) {
			t.Errorf("Recent cache file %s should have been kept", recentCacheFile)
		}
	})
}
