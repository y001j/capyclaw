package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"CapyClaw/internal/riverbank/adapters"
)

// Adapter implements the Telegram bot adapter using long polling.
type Adapter struct {
	bot      *tgbotapi.BotAPI
	botToken string
	handler  adapters.MessageHandler
	stopOnce sync.Once
	stopCh   chan struct{}
}

// New creates a new Telegram adapter.
func New(botToken string, handler adapters.MessageHandler) *Adapter {
	return &Adapter{
		botToken: botToken,
		handler:  handler,
		stopCh:   make(chan struct{}),
	}
}

func (a *Adapter) Name() string { return "telegram" }

func (a *Adapter) Start(ctx context.Context) error {
	bot, err := tgbotapi.NewBotAPI(a.botToken)
	if err != nil {
		return fmt.Errorf("creating Telegram bot: %w", err)
	}
	a.bot = bot

	slog.Info("telegram bot started",
		"username", bot.Self.UserName,
	)

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	updates := bot.GetUpdatesChan(updateConfig)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.stopCh:
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			go a.handleMessage(ctx, update.Message)
		}
	}
}

func (a *Adapter) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	slog.Debug("telegram message received",
		"chat_id", msg.Chat.ID,
		"from", msg.From.UserName,
		"text_length", len(msg.Text),
	)

	incoming := &adapters.IncomingMessage{
		ChannelType: "telegram",
		ChannelID:   strconv.FormatInt(msg.Chat.ID, 10),
		SenderID:    strconv.FormatInt(msg.From.ID, 10),
		SenderName:  buildTelegramName(msg.From),
		Content:     msg.Text,
		Metadata: map[string]string{
			"message_id": strconv.Itoa(msg.MessageID),
			"chat_type":  msg.Chat.Type,
		},
	}

	// Collect attachments
	if msg.Photo != nil && len(msg.Photo) > 0 {
		// Use the largest photo
		photo := msg.Photo[len(msg.Photo)-1]
		incoming.Attachments = append(incoming.Attachments, adapters.Attachment{
			Type:     "image",
			Filename: photo.FileID,
		})
	}
	if msg.Document != nil {
		incoming.Attachments = append(incoming.Attachments, adapters.Attachment{
			Type:     "file",
			Filename: msg.Document.FileName,
			MimeType: msg.Document.MimeType,
		})
	}

	if err := a.handler(ctx, incoming); err != nil {
		slog.Error("telegram handler error",
			"chat_id", msg.Chat.ID,
			"error", err,
		)
	}
}

func (a *Adapter) Stop(ctx context.Context) error {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		if a.bot != nil {
			a.bot.StopReceivingUpdates()
		}
	})
	return nil
}

func (a *Adapter) SendMessage(ctx context.Context, target string, msg *adapters.OutgoingMessage) error {
	if a.bot == nil {
		return fmt.Errorf("telegram bot not initialized")
	}

	chatID, err := strconv.ParseInt(target, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Split long messages (Telegram limit is 4096 chars)
	content := msg.Content
	for len(content) > 0 {
		chunk := content
		if len(chunk) > 4096 {
			// Try to split at a newline
			idx := strings.LastIndex(chunk[:4096], "\n")
			if idx < 0 {
				idx = 4096
			}
			chunk = content[:idx]
			content = content[idx:]
		} else {
			content = ""
		}

		tgMsg := tgbotapi.NewMessage(chatID, chunk)
		if msg.ReplyTo != "" {
			if replyID, err := strconv.Atoi(msg.ReplyTo); err == nil {
				tgMsg.ReplyToMessageID = replyID
			}
		}
		tgMsg.ParseMode = "Markdown"

		if _, err := a.bot.Send(tgMsg); err != nil {
			return fmt.Errorf("sending telegram message: %w", err)
		}
	}

	return nil
}

func buildTelegramName(user *tgbotapi.User) string {
	if user.UserName != "" {
		return user.UserName
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	return name
}
