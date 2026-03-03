package robot

// MessageHandler is a message processing interface called by Feishu/DingTalk long connections (implemented by handler.RobotHandler)
type MessageHandler interface {
	HandleMessage(platform, userID, text string) string
}
