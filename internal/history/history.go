package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/genai"
	"log/slog"
	"os"
)

// Logic .
type Logic struct {
	logger      *slog.Logger
	historyPath string
}

// New .
func New(logger *slog.Logger, historyPath string) *Logic {
	return &Logic{
		logger:      logger,
		historyPath: historyPath,
	}
}

type saveFormat struct {
	History []*genai.Content `json:"history"`
}

// Read .
func (l *Logic) Read(sessionID string) ([]*genai.Content, error) {
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
func (l *Logic) Save(sessionID string, history []*genai.Content) error {
	b, err := json.Marshal(saveFormat{
		History: history,
	})
	if err != nil {
		return err
	}

	return os.WriteFile(fmt.Sprintf("%s/%s", l.historyPath, sessionID), b, 0644)
}
