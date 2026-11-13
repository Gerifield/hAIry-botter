package agent

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"hairy-botter/internal/config"
)

func TestInjectSystemPromptDynamicCommandTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx := context.Background()

	cfg := config.ContextConfig{
		DynamicData: []config.DynamicDataConfig{
			{
				Name:    "Fast command",
				Command: "echo",
				Args:    []string{"hello-world"},
			},
			{
				Name:    "Slow command",
				Command: "sleep",
				Args:    []string{"5"},
				Timeout: 50 * time.Millisecond,
			},
		},
	}

	start := time.Now()
	got := injectSystemPrompt(ctx, logger, "Base prompt", cfg, 100*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("injectSystemPrompt took too long (%v); timeout did not cancel slow command", elapsed)
	}

	if !strings.Contains(got, "[Fast command] hello-world") {
		t.Errorf("expected fast command output in prompt, got: %q", got)
	}
	if strings.Contains(got, "[Slow command]") {
		t.Errorf("expected slow timed-out command to be skipped, got: %q", got)
	}
}
