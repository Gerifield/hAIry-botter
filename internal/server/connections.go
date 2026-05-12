package server

import (
	"context"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsConn) send(ctx context.Context, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(ctx, c.conn, v)
}

// ConnectionManager manages WebSocket connections grouped by session ID.
// Multiple clients may share a session and all receive broadcasts.
type ConnectionManager struct {
	mu       sync.RWMutex
	sessions map[string][]*wsConn
}

func newConnectionManager() *ConnectionManager {
	return &ConnectionManager{sessions: make(map[string][]*wsConn)}
}

func (m *ConnectionManager) register(sessionID string, c *wsConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = append(m.sessions[sessionID], c)
}

func (m *ConnectionManager) unregister(sessionID string, c *wsConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conns := m.sessions[sessionID]
	for i, existing := range conns {
		if existing == c {
			m.sessions[sessionID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(m.sessions[sessionID]) == 0 {
		delete(m.sessions, sessionID)
	}
}

// Broadcast sends msg to every connection registered under sessionID.
func (m *ConnectionManager) Broadcast(ctx context.Context, sessionID string, msg any) {
	m.mu.RLock()
	conns := make([]*wsConn, len(m.sessions[sessionID]))
	copy(conns, m.sessions[sessionID])
	m.mu.RUnlock()

	for _, c := range conns {
		_ = c.send(ctx, msg)
	}
}
