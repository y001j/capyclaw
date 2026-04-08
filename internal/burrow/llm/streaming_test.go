package llm

import (
	"context"
	"strings"
	"testing"
)

func TestSSEParser_BasicEvent(t *testing.T) {
	input := "data: hello world\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "hello world" {
		t.Errorf("data = %q, want 'hello world'", events[0].Data)
	}
}

func TestSSEParser_MultipleEvents(t *testing.T) {
	input := "data: first\n\ndata: second\n\ndata: third\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Data != "first" {
		t.Errorf("events[0].Data = %q", events[0].Data)
	}
	if events[1].Data != "second" {
		t.Errorf("events[1].Data = %q", events[1].Data)
	}
	if events[2].Data != "third" {
		t.Errorf("events[2].Data = %q", events[2].Data)
	}
}

func TestSSEParser_EventType(t *testing.T) {
	input := "event: error\ndata: something failed\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != "error" {
		t.Errorf("event = %q, want 'error'", events[0].Event)
	}
	if events[0].Data != "something failed" {
		t.Errorf("data = %q", events[0].Data)
	}
}

func TestSSEParser_EventID(t *testing.T) {
	input := "id: 42\ndata: test\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != "42" {
		t.Errorf("id = %q, want '42'", events[0].ID)
	}
}

func TestSSEParser_EmptyLinesIgnored(t *testing.T) {
	input := "\n\n\ndata: hello\n\n\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "hello" {
		t.Errorf("data = %q", events[0].Data)
	}
}

func TestSSEParser_JSONData(t *testing.T) {
	input := `data: {"key":"value","nested":{"a":1}}` + "\n\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != `{"key":"value","nested":{"a":1}}` {
		t.Errorf("data = %q", events[0].Data)
	}
}

func TestSSEParser_ContextCancel(t *testing.T) {
	// Create a reader that blocks forever
	input := "data: first\n\n"
	r := strings.NewReader(input)
	parser := NewSSEParser(r)

	ctx, cancel := context.WithCancel(context.Background())
	ch := parser.Parse(ctx)

	// Read first event
	event := <-ch
	if event.Data != "first" {
		t.Errorf("data = %q", event.Data)
	}

	cancel()

	// Channel should close
	for range ch {
	}
}

func TestSSEParser_CarriageReturn(t *testing.T) {
	input := "data: hello\r\n\r\n"
	parser := NewSSEParser(strings.NewReader(input))

	events := collectEvents(t, parser, context.Background())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "hello" {
		t.Errorf("data = %q, want 'hello'", events[0].Data)
	}
}

func TestParseOpenAIChunk_Done(t *testing.T) {
	chunk, err := ParseOpenAIChunk("[DONE]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Type != "done" {
		t.Errorf("type = %s, want done", chunk.Type)
	}
}

func TestParseOpenAIChunk_TextContent(t *testing.T) {
	data := `{"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`
	chunk, err := ParseOpenAIChunk(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Type != "text" {
		t.Errorf("type = %s", chunk.Type)
	}
	if chunk.Content != "Hello" {
		t.Errorf("content = %q", chunk.Content)
	}
}

func TestParseOpenAIChunk_FinishReason(t *testing.T) {
	data := `{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`
	chunk, err := ParseOpenAIChunk(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.FinishReason != "stop" {
		t.Errorf("finish_reason = %s", chunk.FinishReason)
	}
}

func TestParseOpenAIChunk_Usage(t *testing.T) {
	data := `{"choices":[],"usage":{"input_tokens":10,"output_tokens":5}}`
	chunk, err := ParseOpenAIChunk(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("expected usage")
	}
	if chunk.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d", chunk.Usage.InputTokens)
	}
	if chunk.Usage.OutputTokens != 5 {
		t.Errorf("output_tokens = %d", chunk.Usage.OutputTokens)
	}
}

func TestParseOpenAIChunk_InvalidJSON(t *testing.T) {
	_, err := ParseOpenAIChunk("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// collectEvents reads all events from the parser until the channel closes.
func collectEvents(t *testing.T, parser *SSEParser, ctx context.Context) []*SSEEvent {
	t.Helper()
	var events []*SSEEvent
	for event := range parser.Parse(ctx) {
		events = append(events, event)
	}
	return events
}
