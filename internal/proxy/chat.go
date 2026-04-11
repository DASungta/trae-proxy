package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func ChatToAnthropic(data map[string]interface{}, mapModel func(string) string) map[string]interface{} {
	messages, _ := data["messages"].([]interface{})

	var systemParts []string
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "system" {
			systemParts = append(systemParts, FlattenContent(msg["content"]))
		}
	}

	model, _ := data["model"].(string)
	maxTokens := 4096
	if mt, ok := data["max_tokens"].(float64); ok {
		maxTokens = int(mt)
	}

	out := map[string]interface{}{
		"model":      mapModel(model),
		"max_tokens": maxTokens,
		"messages":   ConvertMessages(messages),
	}

	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n")
	}

	for _, field := range []string{"stream", "temperature", "top_p", "stop"} {
		if v, ok := data[field]; ok {
			out[field] = v
		}
	}

	if tools, ok := data["tools"].([]interface{}); ok {
		out["tools"] = ConvertTools(tools)
	}
	if tc, ok := data["tool_choice"]; ok {
		out["tool_choice"] = ConvertToolChoice(tc)
	}

	return out
}

func AnthropicToChat(data map[string]interface{}, origModel string) map[string]interface{} {
	contentBlocks, _ := data["content"].([]interface{})

	var textParts []string
	var toolCalls []map[string]interface{}

	for _, b := range contentBlocks {
		block, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		switch block["type"] {
		case "text":
			if t, ok := block["text"].(string); ok {
				textParts = append(textParts, t)
			}
		case "tool_use":
			id, _ := block["id"].(string)
			if id == "" {
				id = "call_" + ShortID()
			}
			name, _ := block["name"].(string)
			input := block["input"]
			argsBytes, _ := json.Marshal(input)
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(argsBytes),
				},
			})
		}
	}

	stopReason, _ := data["stop_reason"].(string)
	finishMap := map[string]string{
		"end_turn":   "stop",
		"max_tokens": "length",
		"tool_use":   "tool_calls",
	}
	finishReason := finishMap[stopReason]
	if finishReason == "" {
		finishReason = "stop"
	}

	usage, _ := data["usage"].(map[string]interface{})
	inp, _ := usage["input_tokens"].(float64)
	outTok, _ := usage["output_tokens"].(float64)

	text := strings.Join(textParts, "")
	message := map[string]interface{}{
		"role": "assistant",
	}
	if text != "" {
		message["content"] = text
	} else {
		message["content"] = nil
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	id, _ := data["id"].(string)
	if id == "" {
		id = "chatcmpl-" + NewUUID()
	}

	return map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": int(time.Now().Unix()),
		"model":   origModel,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     int(inp),
			"completion_tokens": int(outTok),
			"total_tokens":      int(inp + outTok),
		},
	}
}

func MakeChunk(cid, model string, delta map[string]interface{}, finish *string) string {
	choice := map[string]interface{}{
		"index": 0,
		"delta": delta,
	}
	if finish != nil {
		choice["finish_reason"] = *finish
	} else {
		choice["finish_reason"] = nil
	}

	obj := map[string]interface{}{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": int(time.Now().Unix()),
		"model":   model,
		"choices": []map[string]interface{}{choice},
	}

	b, _ := json.Marshal(obj)
	return fmt.Sprintf("data: %s\n\n", b)
}
