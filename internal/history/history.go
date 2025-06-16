// TODO: we could make this AI agnostic, so get rid of the genai package dependency
package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"google.golang.org/genai"
)

var summarySystemPrompt = &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "You are a summarization AI. Your task is to summarize the conversation history into a single message. Extract all the most important information related to the client like name, phone number and other parameters. It is possible that the model response contains user related information. Only respond with the summarization and keep it short just keep the most important information."}}}
var summeryUserTemplate = "The current history which should be summarized is:\n\n%s"

type Config struct {
	HistorySummary  int           // How many history items to summarize into a single one, 0 means disabled, history contains both user and model messages
	Summarizer      *genai.Client // TODO: We could change this to an interface to decouple from genai package, but let'go for simplicity first
	SummarizerModel string        // Model to use for summarization
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

type saveFormat struct {
	History []*genai.Content `json:"history"`
}

// Read .
func (l *Logic) Read(ctx context.Context, sessionID string) ([]*genai.Content, error) {
	b, err := os.ReadFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) { // Not yet exists, ignore
			return make([]*genai.Content, 0), nil
		}
		return nil, err
	}

	var saved saveFormat
	err = json.Unmarshal(b, &saved)
	if err != nil {
		return nil, err
	}

	return saved.History, nil
}

// Save .
func (l *Logic) Save(ctx context.Context, sessionID string, history []*genai.Content) error {
	var b []byte
	var err error
	if l.config.HistorySummary > 0 && len(history) >= l.config.HistorySummary {
		l.logger.Info("summarizing history", slog.String("sessionID", sessionID), slog.Int("historyLength", len(history)))

		summary, err := l.summarize(ctx, history)
		if err != nil {
			return fmt.Errorf("failed to summarize history: %w", err)
		}

		b, err = json.Marshal(saveFormat{History: []*genai.Content{summary}})
		if err != nil {
			return err
		}
	} else {
		l.logger.Info("saving history", slog.String("sessionID", sessionID), slog.Int("historyLength", len(history)))
		b, err = json.Marshal(saveFormat{History: history})
		if err != nil {
			return err
		}
	}

	return os.WriteFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID), b, 0644)
}

func (l *Logic) summarize(ctx context.Context, history []*genai.Content) (*genai.Content, error) {
	if l.config.HistorySummary == 0 {
		return nil, errors.New("history summarization is disabled")
	}

	if l.config.Summarizer == nil {
		return nil, errors.New("summarizer is not configured")
	}

	if l.config.SummarizerModel == "" {
		return nil, errors.New("summarizer model is not configured")
	}

	historyContent := fmt.Sprintf(summeryUserTemplate, contentToString(history))

	resp, err := l.config.Summarizer.Models.GenerateContent(ctx, l.config.SummarizerModel, genai.Text(historyContent), &genai.GenerateContentConfig{
		SystemInstruction: summarySystemPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	return &genai.Content{
		Role:  "model",
		Parts: []*genai.Part{{Text: fmt.Sprintf("Summarized history:\n\n%s", resp.Text())}},
	}, nil
}

func contentToString(history []*genai.Content) string {
	var res string
	for _, c := range history {
		for _, p := range c.Parts {
			res += fmt.Sprintf("Role: %s, Text:%s\n", c.Role, p.Text)
		}
	}

	return res
}
