// Package wsBotter provides a WebSocket client for the hairy-botter server.
// It maintains a persistent connection to a named session, reconnects with
// exponential backoff on failure, and delivers inbound server messages to a
// caller-supplied handler.
package wsBotter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Message is the wire envelope exchanged with the server.
type Message struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Session string `json:"session,omitempty"`
}

// InlineData carries a binary payload (e.g. an image) alongside a message.
type InlineData struct {
	MimeType string `json:"mime_type"`
	Data     []byte `json:"-"`
	DataB64  string `json:"data"` // populated automatically by Send
}

// outgoing is the client→server envelope.
type outgoing struct {
	Message    string       `json:"message"`
	InlineData []InlineData `json:"inline_data,omitempty"`
}

// Client is a persistent WebSocket connection to one bot session.
type Client struct {
	baseURL   string
	sessionID string
	handler   func(Message)
}

// New creates a new Client. baseURL should be the HTTP root of the bot server
// (e.g. "http://localhost:8080"). handler is called for every inbound Message.
func New(baseURL, sessionID string, handler func(Message)) *Client {
	return &Client{
		baseURL:   baseURL,
		sessionID: sessionID,
		handler:   handler,
	}
}

// Run connects to the server and blocks until ctx is cancelled. It reconnects
// automatically with exponential backoff (1 s → 32 s) on connection failure.
// Inbound messages are delivered to the handler registered at construction time.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if err := c.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Printf("wsBotter: connection error (%v), retrying in %s\n", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 32*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second // reset on clean disconnect
	}
}

func (c *Client) connect(ctx context.Context) error {
	wsURL := fmt.Sprintf("%s/ws/%s", wsBase(c.baseURL), c.sessionID)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	for {
		var msg Message
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return err
		}
		c.handler(msg)
	}
}

func wsBase(baseURL string) string {
	if len(baseURL) >= 5 && baseURL[:5] == "https" {
		return "wss" + baseURL[5:]
	}
	if len(baseURL) >= 4 && baseURL[:4] == "http" {
		return "ws" + baseURL[4:]
	}
	return baseURL
}

// ConnectedClient holds an open WebSocket connection and can send messages on it.
type ConnectedClient struct {
	conn      *websocket.Conn
	sessionID string
}

// Dial opens a connection and returns a ConnectedClient.
func Dial(ctx context.Context, baseURL, sessionID string) (*ConnectedClient, error) {
	wsURL := fmt.Sprintf("%s/ws/%s", wsBase(baseURL), sessionID)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	return &ConnectedClient{conn: conn, sessionID: sessionID}, nil
}

// Send transmits a message and waits for the first response.
// payloads may be nil.
func (c *ConnectedClient) Send(ctx context.Context, msg string, payloads []InlineData) (string, error) {
	for i := range payloads {
		payloads[i].DataB64 = base64.StdEncoding.EncodeToString(payloads[i].Data)
	}

	out := outgoing{Message: msg, InlineData: payloads}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	if err := c.conn.Write(ctx, websocket.MessageText, raw); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}

	var resp Message
	if err := wsjson.Read(ctx, c.conn, &resp); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if resp.Type == "error" {
		return "", fmt.Errorf("server error: %s", resp.Content)
	}
	return resp.Content, nil
}

// ReadAsync starts a goroutine that delivers all inbound messages to handler
// until ctx is cancelled or the connection is closed.
func (c *ConnectedClient) ReadAsync(ctx context.Context, handler func(Message)) {
	go func() {
		for {
			var msg Message
			if err := wsjson.Read(ctx, c.conn, &msg); err != nil {
				return
			}
			handler(msg)
		}
	}()
}

// Close closes the connection.
func (c *ConnectedClient) Close() {
	c.conn.CloseNow()
}

