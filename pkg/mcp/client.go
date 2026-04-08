package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

// Client is a JSON-RPC 2.0 MCP client.
type Client struct {
	transport Transport
	mu        sync.Mutex
	pending   map[string]chan json.RawMessage
	nextID    atomic.Int64
	info      *ServerInfo
}

// NewClient creates a Client using the given transport and performs the
// MCP initialisation handshake.
func NewClient(ctx context.Context, t Transport) (*Client, error) {
	c := &Client{
		transport: t,
		pending:   make(map[string]chan json.RawMessage),
	}
	go c.receiveLoop()

	var info ServerInfo
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "CapyClaw",
			"version": "0.1.0",
		},
	}, &info); err != nil {
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}
	c.info = &info
	return c, nil
}

// ListTools returns the tools exposed by the connected MCP server.
func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	var result struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool invokes a named tool and returns its result.
func (c *Client) CallTool(ctx context.Context, tc ToolCall) (*ToolResult, error) {
	var result ToolResult
	if err := c.call(ctx, "tools/call", tc, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ServerInfo returns metadata about the connected MCP server.
func (c *Client) ServerInfo() *ServerInfo { return c.info }

// Close terminates the connection.
func (c *Client) Close() error { return c.transport.Close() }

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	id := fmt.Sprintf("%d", c.nextID.Add(1))
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.transport.Send(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case raw := <-ch:
		if result != nil {
			return json.Unmarshal(raw, result)
		}
		return nil
	}
}

func (c *Client) receiveLoop() {
	for msg := range c.transport.Receive() {
		var resp struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil || resp.ID == "" {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp.Result
		}
	}
}
