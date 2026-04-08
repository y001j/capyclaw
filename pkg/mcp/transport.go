package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Transport abstracts the wire protocol between the MCP client and server.
// Two standard transports are defined: stdio and SSE.
type Transport interface {
	// Send writes a JSON-encoded message to the server.
	Send(msg []byte) error
	// Receive returns a channel that emits JSON-encoded messages from the server.
	Receive() <-chan []byte
	// Close shuts down the transport.
	Close() error
}

// StdioTransport communicates with an MCP server subprocess over stdin/stdout.
type StdioTransport struct {
	stdin  io.Writer
	stdout io.Reader
	recv   chan []byte
}

// NewStdioTransport wraps existing stdin/stdout streams.
func NewStdioTransport(stdin io.Writer, stdout io.Reader) *StdioTransport {
	t := &StdioTransport{
		stdin:  stdin,
		stdout: stdout,
		recv:   make(chan []byte, 64),
	}
	go t.readLoop()
	return t
}

func (t *StdioTransport) Send(msg []byte) error {
	_, err := t.stdin.Write(append(msg, '\n'))
	return err
}

func (t *StdioTransport) Receive() <-chan []byte { return t.recv }

func (t *StdioTransport) Close() error {
	close(t.recv)
	return nil
}

func (t *StdioTransport) readLoop() {
	buf := make([]byte, 65536)
	for {
		n, err := t.stdout.Read(buf)
		if err != nil {
			return
		}
		msg := make([]byte, n)
		copy(msg, buf[:n])
		t.recv <- msg
	}
}

// SSETransport communicates with a remote MCP server via Server-Sent Events.
// The server streams responses over an SSE connection, and the client sends
// requests via HTTP POST to a message endpoint discovered from the SSE stream.
type SSETransport struct {
	baseURL    string
	messageURL string
	client     *http.Client
	recv       chan []byte
	cancel     context.CancelFunc
	closeOnce  sync.Once
}

// NewSSETransport connects to a remote MCP server at the given URL.
// It establishes an SSE connection to receive messages and discovers
// the POST endpoint for sending requests.
func NewSSETransport(ctx context.Context, url string) (*SSETransport, error) {
	ctx, cancel := context.WithCancel(ctx)
	t := &SSETransport{
		baseURL: url,
		client:  &http.Client{},
		recv:    make(chan []byte, 64),
		cancel:  cancel,
	}

	// Connect to the SSE endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connecting to SSE endpoint: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("SSE endpoint returned status %d", resp.StatusCode)
	}

	// Wait for the endpoint event that tells us where to POST messages
	endpointCh := make(chan string, 1)
	go t.readSSELoop(ctx, resp.Body, endpointCh)

	select {
	case endpoint := <-endpointCh:
		t.messageURL = endpoint
	case <-ctx.Done():
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("timeout waiting for endpoint event: %w", ctx.Err())
	}

	return t, nil
}

func (t *SSETransport) Send(msg []byte) error {
	if t.messageURL == "" {
		return fmt.Errorf("message endpoint not yet discovered")
	}
	resp, err := t.client.Post(t.messageURL, "application/json", bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("sending message: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	return nil
}

func (t *SSETransport) Receive() <-chan []byte { return t.recv }

func (t *SSETransport) Close() error {
	t.closeOnce.Do(func() {
		t.cancel()
		close(t.recv)
	})
	return nil
}

// readSSELoop reads SSE events from the response body.
// The first "endpoint" event provides the POST URL for sending messages.
// Subsequent "message" events are forwarded to the recv channel.
func (t *SSETransport) readSSELoop(ctx context.Context, body io.ReadCloser, endpointCh chan<- string) {
	defer body.Close()
	scanner := bufio.NewScanner(body)

	var eventType string
	var dataBuf bytes.Buffer

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		if line == "" {
			// Empty line = end of event
			if dataBuf.Len() > 0 {
				data := strings.TrimSpace(dataBuf.String())
				dataBuf.Reset()

				switch eventType {
				case "endpoint":
					// Resolve the message URL (may be relative or absolute)
					msgURL := data
					if !strings.HasPrefix(msgURL, "http") {
						// Relative URL — resolve against base
						base := t.baseURL
						if idx := strings.LastIndex(base, "/"); idx > 8 {
							base = base[:idx]
						}
						msgURL = base + "/" + strings.TrimPrefix(msgURL, "/")
					}
					select {
					case endpointCh <- msgURL:
					default:
					}
				case "message", "":
					// Forward JSON-RPC messages
					msg := make([]byte, len(data))
					copy(msg, data)
					select {
					case t.recv <- msg:
					case <-ctx.Done():
						return
					}
				}
				eventType = ""
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}
