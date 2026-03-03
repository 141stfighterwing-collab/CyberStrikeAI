package robot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	dingutils "github.com/open-dingtalk/dingtalk-stream-sdk-go/utils"
	"go.uber.org/zap"
)

const (
	dingReconnectInitial = 5 * time.Second  // First reconnection interval
	dingReconnectMax     = 60 * time.Second // Maximum reconnection interval
)

// StartDing starts the DingTalk Stream long connection (no public network required). After receiving the message, it calls the handler and replies through SessionWebhook.
// It will automatically reconnect after being disconnected (such as laptop sleep, network interruption); exit when ctx is canceled to facilitate restarting when configuration changes.
func StartDing(ctx context.Context, cfg config.RobotDingtalkConfig, h MessageHandler, logger *zap.Logger) {
	if !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return
	}
	go runDingLoop(ctx, cfg, h, logger)
}

// The runDingLoop loop maintains the long connection of DingTalk: when disconnected and ctx is not canceled, the connection is reconnected according to the backoff interval.
func runDingLoop(ctx context.Context, cfg config.RobotDingtalkConfig, h MessageHandler, logger *zap.Logger) {
	backoff := dingReconnectInitial
	for {
		streamClient := client.NewStreamClient(
			client.WithAppCredential(client.NewAppCredentialConfig(cfg.ClientID, cfg.ClientSecret)),
			client.WithSubscription(dingutils.SubscriptionTypeKCallback, "/v1.0/im/bot/messages/get",
				chatbot.NewDefaultChatBotFrameHandler(func(ctx context.Context, msg *chatbot.BotCallbackDataModel) ([]byte, error) {
					go handleDingMessage(ctx, msg, h, logger)
					return nil, nil
				}).OnEventReceived),
		)
		logger.Info("DingTalk Stream is connecting...", zap.String("client_id", cfg.ClientID))
		err := streamClient.Start(ctx)
		if ctx.Err() != nil {
			logger.Info("DingTalk Stream has been restarted and shut down as configured.")
			return
		}
		if err != nil {
			logger.Warn("If the DingTalk Stream long-term connection is disconnected (such as sleep/disconnection), it will automatically reconnect.", zap.Error(err), zap.Duration("retry_after", backoff))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			// The next reconnection interval increases, with an upper limit of 60 seconds, to avoid frequent retries.
			if backoff < dingReconnectMax {
				backoff *= 2
				if backoff > dingReconnectMax {
					backoff = dingReconnectMax
				}
			}
		}
	}
}

func handleDingMessage(ctx context.Context, msg *chatbot.BotCallbackDataModel, h MessageHandler, logger *zap.Logger) {
	if msg == nil || msg.SessionWebhook == "" {
		return
	}
	content := ""
	if msg.Text.Content != "" {
		content = strings.TrimSpace(msg.Text.Content)
	}
	if content == "" && msg.Msgtype == "richText" {
		if cMap, ok := msg.Content.(map[string]interface{}); ok {
			if rich, ok := cMap["richText"].([]interface{}); ok {
				for _, c := range rich {
					if m, ok := c.(map[string]interface{}); ok {
						if txt, ok := m["text"].(string); ok {
							content = strings.TrimSpace(txt)
							break
						}
					}
				}
			}
		}
	}
	if content == "" {
		logger.Debug("DingTalk message content is empty and ignored", zap.String("msgtype", msg.Msgtype))
		return
	}
	logger.Info("DingTalk received the message", zap.String("sender", msg.SenderId), zap.String("content", content))
	userID := msg.SenderId
	if userID == "" {
		userID = msg.ConversationId
	}
	reply := h.HandleMessage("dingtalk", userID, content)
	// Use the markdown type to correctly display titles, lists, code blocks, etc.
	title := reply
	if idx := strings.IndexAny(reply, "\n"); idx > 0 {
		title = strings.TrimSpace(reply[:idx])
	}
	if len(title) > 50 {
		title = title[:50] + "…"
	}
	if title == "" {
		title = "Reply"
	}
	body := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  reply,
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, msg.SessionWebhook, bytes.NewReader(bodyBytes))
	if err != nil {
		logger.Warn("DingTalk structure reply request failed", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Warn("DingTalk reply request failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Warn("DingTalk reply is not 200", zap.Int("status", resp.StatusCode))
		return
	}
	logger.Debug("DingTalk reply successfully", zap.String("content_preview", reply))
}
