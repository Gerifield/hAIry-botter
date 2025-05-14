// Package gemini contains the Gemini implementation of the AI logic
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"google.golang.org/genai"
)

// Logic .
type Logic struct {
	logger *slog.Logger

	client      *genai.Client
	model       string
	historyPath string
	persona     *genai.Content
}

// New .
// TODO: we could use a "history storage" later, now keep it dumb
func New(logger *slog.Logger, client *genai.Client, model string, historyPath string) (*Logic, error) {

	persona, err := readPersonality()
	if err != nil {
		return nil, err
	}

	return &Logic{
		logger:      logger,
		client:      client,
		model:       model,
		historyPath: historyPath,
		persona:     persona,
	}, nil
}

// HandleMessage as an internal logic
// sessionID is unique to be able to get the history
func (l *Logic) HandleMessage(ctx context.Context, sessionID string, msg string) (string, error) {
	if sessionID == "" {
		return "", errors.New("sessionID is empty")
	}

	hist, err := l.readHistory(sessionID)
	if err != nil {
		return "", err
	}

	ch, err := l.client.Chats.Create(ctx, l.model, &genai.GenerateContentConfig{
		SystemInstruction: l.persona,
	}, hist)
	if err != nil {
		return "", err
	}

	resp, err := ch.SendMessage(ctx, genai.Part{Text: msg})
	if err != nil {
		return "", err
	}

	err = l.saveHistory(sessionID, ch.History(false))

	return resp.Text(), err
}

type saveFormat struct {
	History []*genai.Content `json:"history"`
}

func (l *Logic) readHistory(sessionID string) ([]*genai.Content, error) {
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

func (l *Logic) saveHistory(sessionID string, history []*genai.Content) error {
	b, err := json.Marshal(saveFormat{
		History: history,
	})
	if err != nil {
		return err
	}

	return os.WriteFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID), b, 0644)
}

func readPersonality() (*genai.Content, error) {
	b, err := os.ReadFile("personality.json")
	if err != nil {
		return nil, err
	}

	var c genai.Content
	err = json.Unmarshal(b, &c)
	if err != nil {
		return nil, err
	}

	return &c, nil
}
