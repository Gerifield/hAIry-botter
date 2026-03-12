package main

import (
	"context"
	"fmt"
	"hairy-botter/pkg/httpBotter"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Send any text message to the bot after the bot has been started

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		fmt.Println("BOT_TOKEN must be set")

		os.Exit(1)
		return
	}

	aiSrv := os.Getenv("AI_SERVICE")
	if aiSrv == "" {
		aiSrv = "http://127.0.0.1:8080"
	}

	usernameLimits := make([]string, 0)
	if usernameLimitsEnv := os.Getenv("USERNAME_LIMITS"); usernameLimitsEnv != "" {
		for _, u := range strings.Split(usernameLimitsEnv, ",") {
			usernameLimits = append(usernameLimits, strings.TrimSpace(u))
		}
	}

	l := New(aiSrv, usernameLimits)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(l.Handler),
	}

	b, err := bot.New(token, opts...)
	if err != nil {
		fmt.Println(err)

		os.Exit(1)
		return
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

	// Graceful shutdown of HTTP server
	if err := srv.Shutdown(context.Background()); err != nil {
		fmt.Println("HTTP server shutdown error:", err)
	}
}

type Logic struct {
	httpB      *httpBotter.Logic
	userLimits []string
	chatID     int64
	mu         sync.RWMutex
	bot        *bot.Bot
}

func New(baseURL string, userLimit []string) *Logic {
	return &Logic{
		httpB:      httpBotter.New(baseURL),
		userLimits: userLimit,
	}
}

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
	w.Write([]byte("Message sent successfully"))
}

// Handler .
func (l *Logic) Handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	l.mu.Lock()
	l.chatID = update.Message.Chat.ID
	l.mu.Unlock()

	// If we have any limits set, check them
	if len(l.userLimits) > 0 {
		found := false
		for _, u := range l.userLimits {
			if update.Message.From.Username == u {
				found = true
				break
			}
		}

		if !found {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "🙅You are not allowed to use this bot.",
			})

			return
		}
	}

	var payloads [][]byte
	msg := update.Message.Text

	if len(update.Message.Photo) > 0 {
		highResImg := biggestImage(update.Message.Photo)
		fmt.Println("photo file ID:", highResImg.FileID)
		fmt.Printf("photo info: W: %d, H: %d, Size: %d\n", highResImg.Width, highResImg.Height, highResImg.FileSize)
		fmt.Println("caption:", update.Message.Caption)
		f, err := b.GetFile(ctx, &bot.GetFileParams{
			FileID: highResImg.FileID,
		})
		if err != nil {
			fmt.Println("error getting file:", err)
			return
		}

		// Download the file
		dlURL := b.FileDownloadLink(f)
		resp, err := http.Get(dlURL)
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
		payloads = append(payloads, data)

		if update.Message.Caption != "" {
			msg = update.Message.Caption
		}
	}

	fmt.Println("Sending message to AI service:", msg)
	res, err := l.httpB.Send(fmt.Sprintf("tg-%d", update.Message.Chat.ID), msg, payloads)
	if err != nil {
		fmt.Println("error sending message to AI service:", err)
		return
	}

	fmt.Println("AI service response:", res)
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ParseMode: models.ParseModeMarkdown,
		ChatID:    update.Message.Chat.ID,
		Text:      bot.EscapeMarkdownUnescaped(res),
	})
	if err != nil {
		fmt.Println("error sending response back to Telegram:", err)
		return
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
