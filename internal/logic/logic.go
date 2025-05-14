package logic

import "log/slog"

// Logic .
type Logic struct {
	logger *slog.Logger
}

// New .
func New(logger *slog.Logger) *Logic {
	return &Logic{
		logger: logger,
	}
}

func (l *Logic) HandleMessage(msg string) (string, error) {
	return msg, nil
}
