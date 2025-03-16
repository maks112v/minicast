package websocket

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Manager handles WebSocket connections and broadcasting
type Manager struct {
	// WebSocket upgrader
	upgrader websocket.Upgrader

	// Manage connected clients
	clientsMu sync.RWMutex
	clients   map[*websocket.Conn]bool

	// Manage audio source
	sourceMu   sync.RWMutex
	sourceConn *websocket.Conn

	logger *zap.SugaredLogger
}

// NewManager creates a new WebSocket manager
func NewManager(logger *zap.SugaredLogger) *Manager {
	return &Manager{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
		clients: make(map[*websocket.Conn]bool),
		logger:  logger,
	}
}

// HandleSource manages a source connection
func (m *Manager) HandleSource(conn *websocket.Conn) {
	m.logger.Info("Audio source connected")

	m.sourceMu.Lock()
	if m.sourceConn != nil {
		m.sourceMu.Unlock()
		conn.WriteMessage(websocket.TextMessage, []byte("Another source is already connected"))
		conn.Close()
		return
	}
	m.sourceConn = conn
	m.sourceMu.Unlock()

	defer func() {
		m.sourceMu.Lock()
		if m.sourceConn == conn {
			m.sourceConn = nil
		}
		m.sourceMu.Unlock()
		conn.Close()
		m.logger.Info("Audio source disconnected")
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.logger.Errorf("Source WebSocket error: %v", err)
			}
			break
		}

		if messageType == websocket.BinaryMessage {
			m.Broadcast(data)
		}
	}
}

// HandleListener manages a listener connection
func (m *Manager) HandleListener(conn *websocket.Conn) {
	m.logger.Info("Listener connected")

	m.clientsMu.Lock()
	m.clients[conn] = true
	m.clientsMu.Unlock()

	defer func() {
		m.clientsMu.Lock()
		delete(m.clients, conn)
		m.clientsMu.Unlock()
		conn.Close()
		m.logger.Info("Listener disconnected")
	}()

	// Keep the connection alive and handle any incoming messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.logger.Debugf("Listener WebSocket error: %v", err)
			}
			break
		}
	}
}

// Broadcast sends data to all connected listeners
func (m *Manager) Broadcast(data []byte) {
	m.clientsMu.RLock()
	defer m.clientsMu.RUnlock()

	for client := range m.clients {
		err := client.WriteMessage(websocket.BinaryMessage, data)
		if err != nil {
			m.logger.Debugf("Error sending to listener: %v", err)
			client.Close()
			delete(m.clients, client)
		}
	}
}

// GetUpgrader returns the WebSocket upgrader
func (m *Manager) GetUpgrader() *websocket.Upgrader {
	return &m.upgrader
}
