package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"hairy-botter/pkg/wsBotter"
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		fmt.Println("BOT_TOKEN must be set")
		os.Exit(1)
	}

	aiSrv := os.Getenv("AI_SERVICE")
	if aiSrv == "" {
		aiSrv = "http://127.0.0.1:8080"
	}

	usernameLimits := make([]string, 0)
	if env := os.Getenv("USERNAME_LIMITS"); env != "" {
		for _, u := range strings.Split(env, ",") {
			usernameLimits = append(usernameLimits, strings.TrimSpace(u))
		}
	}

	l := New(aiSrv, usernameLimits)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(l.Handler),
	}

	b, err := bot.New(token, opts...)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	l.bot = b

	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", l.httpHandler)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		fmt.Println("Starting HTTP server on port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("HTTP server error:", err)
		}
	}()

	b.Start(ctx)

	l.closeAll()

	if err := srv.Shutdown(context.Background()); err != nil {
		fmt.Println("HTTP server shutdown error:", err)
	}
}

// Logic holds per-instance state for the Telegram bot.
type Logic struct {
	aiSrv      string
	userLimits []string
	bot        *bot.Bot

	// chatID is tracked so the push-notification HTTP handler can deliver
	// messages to the most recently active chat.
	mu     sync.RWMutex
	chatID int64

	// sessions maps "tg-{chatID}" → open WebSocket connection.
	sessionMu sync.Mutex
	sessions  map[string]*wsBotter.ConnectedClient
}

// New creates a Logic ready to be wired to a Telegram bot.
func New(aiSrv string, userLimit []string) *Logic {
	return &Logic{
		aiSrv:      aiSrv,
		userLimits: userLimit,
		sessions:   make(map[string]*wsBotter.ConnectedClient),
	}
}

// session returns the cached WebSocket connection for sessionID, dialling a
// new one if necessary.
func (l *Logic) session(ctx context.Context, sessionID string) (*wsBotter.ConnectedClient, error) {
	l.sessionMu.Lock()
	defer l.sessionMu.Unlock()

	if c, ok := l.sessions[sessionID]; ok {
		return c, nil
	}

	c, err := wsBotter.Dial(ctx, l.aiSrv, sessionID)
	if err != nil {
		return nil, err
	}
	l.sessions[sessionID] = c
	return c, nil
}

// closeAll closes every open WebSocket session (called on shutdown).
func (l *Logic) closeAll() {
	l.sessionMu.Lock()
	defer l.sessionMu.Unlock()
	for id, c := range l.sessions {
		c.Close()
		delete(l.sessions, id)
	}
}

// httpHandler lets external systems push a message to the most-recently-active
// Telegram chat via POST / with a `payload` form field.
func (l *Logic) httpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	payload := r.FormValue("payload")
	if payload == "" {
		http.Error(w, "Empty payload", http.StatusBadRequest)
		return
	}

	l.mu.RLock()
	chatID := l.chatID
	l.mu.RUnlock()

	if chatID == 0 {
		http.Error(w, "No active chat found. Please send a message to the bot first.", http.StatusServiceUnavailable)
		return
	}

	_, err := l.bot.SendMessage(r.Context(), &bot.SendMessageParams{
		ParseMode: models.ParseModeMarkdown,
		ChatID:    chatID,
		Text:      bot.EscapeMarkdownUnescaped(payload),
	})
	if err != nil {
		fmt.Println("Error sending HTTP payload to Telegram:", err)
		http.Error(w, "Failed to send message to Telegram", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Message sent successfully"))
}

// Handler is the Telegram bot update handler.
func (l *Logic) Handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	if len(l.userLimits) > 0 {
		allowed := false
		for _, u := range l.userLimits {
			if update.Message.From.Username == u {
				allowed = true
				break
			}
		}
		if !allowed {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "You are not allowed to use this bot.",
			})
			return
		}
	}

	l.mu.Lock()
	l.chatID = update.Message.Chat.ID
	l.mu.Unlock()

	chatID := update.Message.Chat.ID
	sessionID := fmt.Sprintf("tg-%d", chatID)

	var payloads []wsBotter.InlineData
	msg := update.Message.Text

	if len(update.Message.Photo) > 0 {
		photo := biggestImage(update.Message.Photo)
		f, err := b.GetFile(ctx, &bot.GetFileParams{FileID: photo.FileID})
		if err != nil {
			fmt.Println("error getting file:", err)
			return
		}

		resp, err := http.Get(b.FileDownloadLink(f))
		if err != nil {
			fmt.Println("error downloading file:", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println("error reading file:", err)
			return
		}

		payloads = append(payloads, wsBotter.InlineData{
			MimeType: http.DetectContentType(data),
			Data:     data,
		})

		if update.Message.Caption != "" {
			msg = update.Message.Caption
		}
	}

	// Send a single typing action; Telegram shows it for ~5 s then it expires
	// automatically — no need for a background loop.
	_, _ = b.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionTyping,
	})

	wsClient, err := l.session(ctx, sessionID)
	if err != nil {
		fmt.Println("error connecting to AI service:", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Sorry, I could not connect to the AI service.",
		})
		return
	}

	res, err := wsClient.Send(ctx, msg, payloads)
	if err != nil {
		fmt.Println("error sending message to AI service:", err)

		// Invalidate the broken connection so the next request re-dials.
		l.sessionMu.Lock()
		delete(l.sessions, sessionID)
		l.sessionMu.Unlock()

		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Sorry, I encountered an error: %v", err),
		})
		return
	}

	fmt.Println("AI service response:", res)
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ParseMode: models.ParseModeMarkdown,
		ChatID:    chatID,
		Text:      bot.EscapeMarkdownUnescaped(res),
	})
	if err != nil {
		fmt.Println("error sending response back to Telegram:", err)
	}
}

func biggestImage(photos []models.PhotoSize) models.PhotoSize {
	if len(photos) == 0 {
		return models.PhotoSize{}
	}
	biggest := photos[0]
	for _, photo := range photos {
		if photo.FileSize > biggest.FileSize {
			biggest = photo
		}
	}
	return biggest
}
