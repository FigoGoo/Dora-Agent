package eino

import "github.com/cloudwego/eino/schema"

type Message = schema.Message

func UserPrompt(text string) *Message {
	return schema.UserMessage(text)
}

func AssistantMessage(text string) *Message {
	return schema.AssistantMessage(text, nil)
}
