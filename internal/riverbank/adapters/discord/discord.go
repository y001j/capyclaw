package discord

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"

	"CapyClaw/internal/riverbank/adapters"
)

// Adapter implements the Discord bot adapter using the Gateway WebSocket.
type Adapter struct {
	session  *discordgo.Session
	botToken string
	guildID  string
	handler  adapters.MessageHandler
	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

// New creates a new Discord adapter.
func New(botToken, guildID string, handler adapters.MessageHandler) *Adapter {
	return &Adapter{
		botToken: botToken,
		guildID:  guildID,
		handler:  handler,
	}
}

func (a *Adapter) Name() string { return "discord" }

func (a *Adapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	session, err := discordgo.New("Bot " + a.botToken)
	if err != nil {
		return fmt.Errorf("creating Discord session: %w", err)
	}
	a.session = session

	// Set intents for message content
	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent

	// Register message handler
	session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore messages from the bot itself
		if m.Author.ID == s.State.User.ID {
			return
		}
		go a.handleMessage(m)
	})

	if err := session.Open(); err != nil {
		return fmt.Errorf("opening Discord gateway: %w", err)
	}

	slog.Info("discord bot started",
		"username", session.State.User.Username,
		"guild_id", a.guildID,
	)

	// Block until context is cancelled
	<-a.ctx.Done()
	return nil
}

func (a *Adapter) handleMessage(m *discordgo.MessageCreate) {
	slog.Debug("discord message received",
		"channel_id", m.ChannelID,
		"author", m.Author.Username,
		"content_length", len(m.Content),
	)

	incoming := &adapters.IncomingMessage{
		ChannelType: "discord",
		ChannelID:   m.ChannelID,
		SenderID:    m.Author.ID,
		SenderName:  m.Author.Username,
		Content:     m.Content,
		Metadata: map[string]string{
			"message_id": m.ID,
			"guild_id":   m.GuildID,
		},
	}

	// Collect attachments
	for _, att := range m.Attachments {
		incoming.Attachments = append(incoming.Attachments, adapters.Attachment{
			Type:     classifyAttachment(att.ContentType),
			URL:      att.URL,
			Filename: att.Filename,
			MimeType: att.ContentType,
		})
	}

	if err := a.handler(a.ctx, incoming); err != nil {
		slog.Error("discord handler error",
			"channel_id", m.ChannelID,
			"error", err,
		)
	}
}

func (a *Adapter) Stop(ctx context.Context) error {
	a.stopOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		if a.session != nil {
			a.session.Close()
		}
	})
	return nil
}

func (a *Adapter) SendMessage(ctx context.Context, target string, msg *adapters.OutgoingMessage) error {
	if a.session == nil {
		return fmt.Errorf("discord session not initialized")
	}

	// Discord message limit is 2000 characters
	content := msg.Content
	for len(content) > 0 {
		chunk := content
		if len(chunk) > 2000 {
			chunk = content[:2000]
			content = content[2000:]
		} else {
			content = ""
		}

		sendMsg := &discordgo.MessageSend{
			Content: chunk,
		}

		// Reply in thread if reply_to is set
		if msg.ReplyTo != "" {
			sendMsg.Reference = &discordgo.MessageReference{
				MessageID: msg.ReplyTo,
			}
		}

		if _, err := a.session.ChannelMessageSendComplex(target, sendMsg); err != nil {
			return fmt.Errorf("sending discord message: %w", err)
		}
	}

	return nil
}

func classifyAttachment(contentType string) string {
	switch {
	case contentType == "":
		return "file"
	case len(contentType) >= 5 && contentType[:5] == "image":
		return "image"
	case len(contentType) >= 5 && contentType[:5] == "video":
		return "video"
	case len(contentType) >= 5 && contentType[:5] == "audio":
		return "audio"
	default:
		return "file"
	}
}
