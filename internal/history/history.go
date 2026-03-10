// TODO: we could make this AI agnostic, so get rid of the genai package dependency
package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/firebase/genkit/go/ai"
)

var summarySystemPrompt = "You are a summarization AI. Your task is to summarize the conversation history into a single message. Extract all the most important information related to the client like name, phone number and other parameters. It is possible that the model response contains user related information. Only respond with the summarization and keep it short just keep the most important information."
var summeryUserTemplate = "The current history which should be summarized is:\n\n%s"

// Summarizer abstracts the summarization logic so the history package stays AI-framework agnostic.
type Summarizer interface {
	Summarize(ctx context.Context, systemPrompt, text string) (string, error)
}

type Config struct {
	HistorySummary int       // How many history items to summarize into a single one, 0 means disabled, history contains both user and model messages
	Summarizer     Summarizer // If nil, summarization is disabled even when HistorySummary > 0
	SummarizerNote string    // Optional: model name for logging
}

// Logic .
type Logic struct {
	logger      *slog.Logger
	historyPath string
	config      Config
}

// New .
func New(logger *slog.Logger, historyPath string, config Config) *Logic {
	return &Logic{
		logger:      logger,
		historyPath: historyPath,
		config:      config,
	}
}

type legacyContent struct {
	Role  string `json:"role"`
	Parts []struct {
		Text string `json:"text"`
	} `json:"parts"`
}

type saveFormat struct {
	History json.RawMessage `json:"history"`
}

// Read .
func (l *Logic) Read(ctx context.Context, sessionID string) ([]*ai.Message, error) {
	b, err := os.ReadFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) { // Not yet exists, ignore
			return make([]*ai.Message, 0), nil
		}
		return nil, err
	}

	var saved saveFormat
	err = json.Unmarshal(b, &saved)
	if err != nil {
		return nil, err
	}

	// Try unmarshaling into []*ai.Message (Genkit format)
	var history []*ai.Message
	if err := json.Unmarshal(saved.History, &history); err == nil && len(history) > 0 && len(history[0].Content) > 0 {
		return history, nil
	}

	// Fallback: Try unmarshaling from legacy genai.Content format
	var legacy []*legacyContent
	if err := json.Unmarshal(saved.History, &legacy); err == nil {
		history = make([]*ai.Message, 0, len(legacy))
		for _, lc := range legacy {
			parts := make([]*ai.Part, 0, len(lc.Parts))
			for _, p := range lc.Parts {
				parts = append(parts, ai.NewTextPart(p.Text))
			}
			history = append(history, &ai.Message{
				Role:    ai.Role(lc.Role),
				Content: parts,
			})
		}
		return history, nil
	}

	return nil, errors.New("failed to unmarshal history: unknown format")
}

// Save .
func (l *Logic) Save(ctx context.Context, sessionID string, history []*ai.Message) error {
	var b []byte
	var err error

	type saveFormatNative struct {
		History []*ai.Message `json:"history"`
	}

	if l.config.HistorySummary > 0 && len(history) >= l.config.HistorySummary {
		l.logger.Info("summarizing history", slog.String("sessionID", sessionID), slog.Int("historyLength", len(history)))

		summary, err := l.summarize(ctx, history)
		if err != nil {
			return fmt.Errorf("failed to summarize history: %w", err)
		}

		b, err = json.Marshal(saveFormatNative{History: []*ai.Message{summary}})
		if err != nil {
			return err
		}
	} else {
		l.logger.Info("saving history", slog.String("sessionID", sessionID), slog.Int("historyLength", len(history)))
		b, err = json.Marshal(saveFormatNative{History: history})
		if err != nil {
			return err
		}
	}

	return os.WriteFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID), b, 0644)
}

func (l *Logic) summarize(ctx context.Context, history []*ai.Message) (*ai.Message, error) {
	if l.config.HistorySummary == 0 {
		return nil, errors.New("history summarization is disabled")
	}

	if l.config.Summarizer == nil {
		return nil, errors.New("summarizer is not configured")
	}

	historyContent := fmt.Sprintf(summeryUserTemplate, messagesToString(history))

	summary, err := l.config.Summarizer.Summarize(ctx, summarySystemPrompt, historyContent)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	return ai.NewModelTextMessage(fmt.Sprintf("Summarized history:\n\n%s", summary)), nil
}

func messagesToString(history []*ai.Message) string {
	var res string
	for _, m := range history {
		for _, p := range m.Content {
			if p.IsText() {
				res += fmt.Sprintf("Role: %s, Text:%s\n", m.Role, p.Text)
			}
		}
	}

	return res
}
