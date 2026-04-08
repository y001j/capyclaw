package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// MessageHandler processes incoming WebSocket JSON-RPC requests.
type MessageHandler interface {
	HandleChatSend(client *Client, req *Request)
	HandleChatCancel(client *Client, req *Request)
	HandleSessionGet(client *Client, req *Request)
	HandleSessionsList(client *Client, req *Request)
	HandleAgentsList(client *Client, req *Request)
	HandleAgentsGet(client *Client, req *Request)
}

// Handler manages WebSocket connections and message routing.
type Handler struct {
	hub     *Hub
	opts    *websocket.AcceptOptions
	msgHandler MessageHandler
}

// NewHandler creates a new WebSocket handler.
func NewHandler(allowedOrigins []string, msgHandler MessageHandler) *Handler {
	return &Handler{
		hub: NewHub(),
		opts: &websocket.AcceptOptions{
			OriginPatterns: allowedOrigins,
		},
		msgHandler: msgHandler,
	}
}

// Hub returns the connection hub.
func (h *Handler) Hub() *Hub { return h.hub }

// ServeHTTP upgrades HTTP connections to WebSocket and manages the connection lifecycle.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, h.opts)
	if err != nil {
		slog.Error("websocket accept failed", "error", err)
		return
	}

	client := &Client{
		conn: conn,
		hub:  h.hub,
		send: make(chan []byte, 256),
	}

	h.hub.Register(client)
	defer h.hub.Unregister(client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		client.readPump(ctx, h.msgHandler)
		cancel()
	}()

	go func() {
		defer wg.Done()
		client.writePump(ctx)
	}()

	wg.Wait()
	conn.Close(websocket.StatusNormalClosure, "connection closed")
}

// Client represents a connected WebSocket client.
type Client struct {
	conn *websocket.Conn
	hub  *Hub
	send chan []byte

	// Connection metadata set after handshake
	UserID   string
	TenantID string
	Role     string
	DeviceID string
}

// InitConn initializes the client's connection and hub reference.
// Used when the Client is created externally (e.g., in the gateway handler).
func (c *Client) InitConn(conn *websocket.Conn, hub *Hub) {
	c.conn = conn
	c.hub = hub
	if c.send == nil {
		c.send = make(chan []byte, 256)
	}
}

// RunReadPump is the public entry point for the read pump goroutine.
func (c *Client) RunReadPump(ctx context.Context, handler MessageHandler) {
	c.readPump(ctx, handler)
}

// RunWritePump is the public entry point for the write pump goroutine.
func (c *Client) RunWritePump(ctx context.Context) {
	c.writePump(ctx)
}

// Send enqueues a message for delivery to the client.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		slog.Warn("client send buffer full, dropping message", "user_id", c.UserID)
	}
}

// SendResponse sends a JSON-RPC response to the client.
func (c *Client) SendResponse(id string, ok bool, payload any, errPayload *ErrorPayload) {
	resp := Response{
		Type:  FrameTypeResponse,
		ID:    id,
		OK:    ok,
		Error: errPayload,
	}
	if payload != nil {
		data, _ := json.Marshal(payload)
		resp.Payload = data
	}
	raw, _ := json.Marshal(resp)
	c.Send(raw)
}

// SendResult sends a successful JSON-RPC response.
func (c *Client) SendResult(id string, payload any) {
	c.SendResponse(id, true, payload, nil)
}

// SendError sends an error JSON-RPC response.
func (c *Client) SendError(id string, code int, message string) {
	c.SendResponse(id, false, nil, &ErrorPayload{
		Code:    code,
		Message: message,
	})
}

// SendEvent sends a server-initiated event to the client.
func (c *Client) SendEvent(event string, payload any, seq int64) {
	evt := Event{
		Type:  FrameTypeEvent,
		Event: event,
		Seq:   seq,
	}
	if payload != nil {
		data, _ := json.Marshal(payload)
		evt.Payload = data
	}
	raw, _ := json.Marshal(evt)
	c.Send(raw)
}

func (c *Client) readPump(ctx context.Context, handler MessageHandler) {
	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				slog.Debug("websocket closed", "status", websocket.CloseStatus(err))
			} else {
				slog.Error("websocket read error", "error", err)
			}
			return
		}

		// Parse JSON-RPC frame
		var frame Request
		if err := json.Unmarshal(msg, &frame); err != nil {
			c.SendError("", ErrCodeBadRequest, "invalid JSON-RPC frame")
			continue
		}

		if frame.Method == "" {
			c.SendError(frame.ID, ErrCodeBadRequest, "method is required")
			continue
		}

		// Method routing
		switch frame.Method {
		case MethodChatSend:
			go handler.HandleChatSend(c, &frame)
		case MethodChatCancel:
			go handler.HandleChatCancel(c, &frame)
		case MethodSessionsGet:
			go handler.HandleSessionGet(c, &frame)
		case MethodSessionsList:
			go handler.HandleSessionsList(c, &frame)
		case MethodAgentsList:
			go handler.HandleAgentsList(c, &frame)
		case MethodAgentsGet:
			go handler.HandleAgentsGet(c, &frame)
		case "ping":
			c.SendResult(frame.ID, map[string]string{"pong": "ok"})
		default:
			c.SendError(frame.ID, ErrCodeBadRequest, "unknown method: "+frame.Method)
		}
	}
}

func (c *Client) writePump(ctx context.Context) {
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				slog.Error("websocket write error", "error", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
