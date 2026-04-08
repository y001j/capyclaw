package adapters

import "context"

// ChannelAdapter defines the interface for messaging platform integrations.
type ChannelAdapter interface {
	// Name returns the adapter name (e.g., "telegram", "discord").
	Name() string

	// Start initializes and starts the adapter's event loop.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the adapter.
	Stop(ctx context.Context) error

	// SendMessage sends a message to a specific channel/chat.
	SendMessage(ctx context.Context, target string, msg *OutgoingMessage) error
}

// IncomingMessage represents a message received from a channel.
type IncomingMessage struct {
	ChannelType string            `json:"channel_type"`
	ChannelID   string            `json:"channel_id"`
	SenderID    string            `json:"sender_id"`
	SenderName  string            `json:"sender_name"`
	Content     string            `json:"content"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// OutgoingMessage represents a message to send to a channel.
type OutgoingMessage struct {
	Content     string       `json:"content"`
	Attachments []Attachment `json:"attachments,omitempty"`
	ReplyTo     string       `json:"reply_to,omitempty"`
}

// Attachment represents a file or media attachment.
type Attachment struct {
	Type     string `json:"type"` // image, file, audio, video
	URL      string `json:"url,omitempty"`
	Data     []byte `json:"-"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// MessageHandler is called when an adapter receives a message.
type MessageHandler func(ctx context.Context, msg *IncomingMessage) error
