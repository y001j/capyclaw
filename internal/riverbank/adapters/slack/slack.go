package slack

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"CapyClaw/internal/riverbank/adapters"
)

// Adapter implements the Slack bot adapter using Socket Mode.
type Adapter struct {
	client   *slack.Client
	socket   *socketmode.Client
	botToken string
	appToken string
	handler  adapters.MessageHandler
	botID    string
	stopOnce sync.Once
	cancel   context.CancelFunc
}

// New creates a new Slack adapter.
func New(botToken, appToken string, handler adapters.MessageHandler) *Adapter {
	return &Adapter{
		botToken: botToken,
		appToken: appToken,
		handler:  handler,
	}
}

func (a *Adapter) Name() string { return "slack" }

func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	a.client = slack.New(
		a.botToken,
		slack.OptionAppLevelToken(a.appToken),
	)
	a.socket = socketmode.New(a.client)

	// Get bot user ID to avoid responding to self
	authResp, err := a.client.AuthTest()
	if err != nil {
		cancel()
		return fmt.Errorf("slack auth test: %w", err)
	}
	a.botID = authResp.UserID

	slog.Info("slack bot started",
		"bot_id", a.botID,
		"team", authResp.Team,
	)

	// Handle socket mode events
	go a.handleEvents(ctx)

	// This blocks until context is cancelled
	return a.socket.RunContext(ctx)
}

func (a *Adapter) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-a.socket.Events:
			if !ok {
				return
			}
			a.processEvent(ctx, evt)
		}
	}
}

func (a *Adapter) processEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		a.socket.Ack(*evt.Request)

		switch eventsAPIEvent.Type {
		case slackevents.CallbackEvent:
			innerEvent := eventsAPIEvent.InnerEvent
			switch ev := innerEvent.Data.(type) {
			case *slackevents.MessageEvent:
				// Ignore bot messages
				if ev.User == a.botID || ev.BotID != "" {
					return
				}
				go a.handleMessage(ctx, ev)
			case *slackevents.AppMentionEvent:
				if ev.User == a.botID {
					return
				}
				go a.handleMention(ctx, ev)
			}
		}
	}
}

func (a *Adapter) handleMessage(ctx context.Context, ev *slackevents.MessageEvent) {
	slog.Debug("slack message received",
		"channel", ev.Channel,
		"user", ev.User,
		"text_length", len(ev.Text),
	)

	incoming := &adapters.IncomingMessage{
		ChannelType: "slack",
		ChannelID:   ev.Channel,
		SenderID:    ev.User,
		Content:     ev.Text,
		Metadata: map[string]string{
			"thread_ts": ev.ThreadTimeStamp,
			"ts":        ev.TimeStamp,
		},
	}

	// Resolve user display name
	if user, err := a.client.GetUserInfo(ev.User); err == nil {
		incoming.SenderName = user.RealName
	}

	if err := a.handler(ctx, incoming); err != nil {
		slog.Error("slack handler error",
			"channel", ev.Channel,
			"error", err,
		)
	}
}

func (a *Adapter) handleMention(ctx context.Context, ev *slackevents.AppMentionEvent) {
	slog.Debug("slack mention received",
		"channel", ev.Channel,
		"user", ev.User,
	)

	incoming := &adapters.IncomingMessage{
		ChannelType: "slack",
		ChannelID:   ev.Channel,
		SenderID:    ev.User,
		Content:     ev.Text,
		Metadata: map[string]string{
			"thread_ts": ev.ThreadTimeStamp,
			"ts":        ev.TimeStamp,
			"type":      "mention",
		},
	}

	if user, err := a.client.GetUserInfo(ev.User); err == nil {
		incoming.SenderName = user.RealName
	}

	if err := a.handler(ctx, incoming); err != nil {
		slog.Error("slack mention handler error",
			"channel", ev.Channel,
			"error", err,
		)
	}
}

func (a *Adapter) Stop(ctx context.Context) error {
	a.stopOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
	})
	return nil
}

func (a *Adapter) SendMessage(ctx context.Context, target string, msg *adapters.OutgoingMessage) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(msg.Content, false),
	}

	// Reply in thread if thread_ts is set
	if msg.ReplyTo != "" {
		opts = append(opts, slack.MsgOptionTS(msg.ReplyTo))
	}

	_, _, err := a.client.PostMessageContext(ctx, target, opts...)
	if err != nil {
		return fmt.Errorf("sending slack message: %w", err)
	}

	return nil
}

