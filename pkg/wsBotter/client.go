package wsBotter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type WSRequest struct {
	Message    string         `json:"message"`
	InlineData []WSInlineData `json:"inline_data,omitempty"`
}

type WSInlineData struct {
	MimeType string `json:"mime_type"`
	Data     []byte `json:"data"` // This will automatically be base64 encoded/decoded by encoding/json
}

type WSResponse struct {
	Response string `json:"response"`
}

type WSClient struct {
	baseURL   string
	sessionID string

	connMu  sync.Mutex
	writeMu sync.Mutex
	conn    *websocket.Conn

	ctx    context.Context
	cancel context.CancelFunc

	onMessage func(msg string)
}

func New(baseURL, sessionID string) *WSClient {
	// e.g. http://127.0.0.1:8080 -> ws://127.0.0.1:8080
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)

	ctx, cancel := context.WithCancel(context.Background())

	return &WSClient{
		baseURL:   wsURL,
		sessionID: sessionID,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (c *WSClient) OnMessage(fn func(msg string)) {
	c.onMessage = fn
}

func (c *WSClient) Connect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	url := fmt.Sprintf("%s/ws/%s", c.baseURL, c.sessionID)
	conn, _, err := websocket.Dial(c.ctx, url, nil)
	if err != nil {
		return err
	}

	c.conn = conn

	go c.readLoop()

	return nil
}

func (c *WSClient) readLoop() {
	for {
		c.connMu.Lock()
		conn := c.conn
		c.connMu.Unlock()

		if conn == nil {
			return
		}

		var res WSResponse
		err := wsjson.Read(c.ctx, conn, &res)
		if err != nil {
			// Connection error or closed
			c.connMu.Lock()
			if c.conn == conn {
				c.conn.CloseNow()
				c.conn = nil
			}
			c.connMu.Unlock()

			// Reconnect loop if context is not canceled
			go c.reconnect()
			return
		}

		if c.onMessage != nil {
			c.onMessage(res.Response)
		}
	}
}

func (c *WSClient) reconnect() {
	backoff := 1 * time.Second
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			err := c.Connect()
			if err == nil {
				return // Reconnected successfully
			}
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (c *WSClient) Send(msg string, payloads [][]byte) error {
	// Ensure connected
	err := c.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()

	if conn == nil {
		return fmt.Errorf("no active connection")
	}

	req := WSRequest{
		Message: msg,
	}

	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		// Basic mime type detection
		mimeType := http.DetectContentType(payload)
		req.InlineData = append(req.InlineData, WSInlineData{
			MimeType: mimeType,
			Data:     payload, // json.Marshal does base64
		})
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	err = wsjson.Write(c.ctx, conn, req)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

func (c *WSClient) Close() {
	c.cancel()
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		c.conn.Close(websocket.StatusNormalClosure, "shutting down")
		c.conn = nil
	}
}
