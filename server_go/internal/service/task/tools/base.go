package tools

import "github.com/sashabaranov/go-openai"

type Tool interface {
	Name() string
	Schema() openai.FunctionDefinition
	Execute(args map[string]interface{}) (string, error)
}
