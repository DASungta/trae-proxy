package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

type StreamConverter struct {
	Model   string
	CID     string
	started bool
	done    bool
	toolIdx map[int]string
}

func NewStreamConverter(origModel string) *StreamConverter {
	return &StreamConverter{
		Model:   origModel,
		CID:     "chatcmpl-" + NewUUID(),
		toolIdx: make(map[int]string),
	}
}

func (sc *StreamConverter) Feed(line string) string {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "data:") {
		return ""
	}
	payload := strings.TrimSpace(line[5:])
	if payload == "" {
		return ""
	}

	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return ""
	}

	evType, _ := ev["type"].(string)
	var out strings.Builder

	switch evType {
	case "message_start":
		if !sc.started {
			sc.started = true
			out.WriteString(MakeChunk(sc.CID, sc.Model, map[string]interface{}{
				"role":    "assistant",
				"content": "",
			}, nil))
		}

	case "content_block_start":
		block, _ := ev["content_block"].(map[string]interface{})
		if block != nil && block["type"] == "tool_use" {
			idx := intFromJSON(ev["index"])
			callID, _ := block["id"].(string)
			if callID == "" {
				callID = "call_" + ShortID()
			}
			sc.toolIdx[idx] = callID
			name, _ := block["name"].(string)
			out.WriteString(MakeChunk(sc.CID, sc.Model, map[string]interface{}{
				"tool_calls": []map[string]interface{}{
					{
						"index": idx,
						"id":    callID,
						"type":  "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": "",
						},
					},
				},
			}, nil))
		}

	case "content_block_delta":
		delta, _ := ev["delta"].(map[string]interface{})
		if delta == nil {
			break
		}
		deltaType, _ := delta["type"].(string)
		switch deltaType {
		case "text_delta":
			text, _ := delta["text"].(string)
			out.WriteString(MakeChunk(sc.CID, sc.Model, map[string]interface{}{
				"content": text,
			}, nil))
		case "input_json_delta":
			idx := intFromJSON(ev["index"])
			partialJSON, _ := delta["partial_json"].(string)
			out.WriteString(MakeChunk(sc.CID, sc.Model, map[string]interface{}{
				"tool_calls": []map[string]interface{}{
					{
						"index": idx,
						"function": map[string]interface{}{
							"arguments": partialJSON,
						},
					},
				},
			}, nil))
		}

	case "message_delta":
		delta, _ := ev["delta"].(map[string]interface{})
		stopReason, _ := delta["stop_reason"].(string)
		finishMap := map[string]string{
			"end_turn":   "stop",
			"max_tokens": "length",
			"tool_use":   "tool_calls",
		}
		finish := finishMap[stopReason]
		if finish == "" {
			finish = "stop"
		}
		out.WriteString(MakeChunk(sc.CID, sc.Model, map[string]interface{}{}, &finish))

	case "message_stop":
		if !sc.done {
			sc.done = true
			out.WriteString("data: [DONE]\n\n")
		}
	}

	return out.String()
}

func intFromJSON(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func ParseSSEEvents(raw string) []string {
	var events []string
	lines := strings.Split(raw, "\n")
	var current strings.Builder
	inData := false

	for _, line := range lines {
		if strings.HasPrefix(line, "event:") {
			if current.Len() > 0 {
				events = append(events, current.String())
				current.Reset()
			}
			inData = false
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if inData && current.Len() > 0 {
				events = append(events, current.String())
				current.Reset()
			}
			inData = true
			current.WriteString(line)
			continue
		}
		if inData && strings.TrimSpace(line) != "" {
			payload := strings.TrimSpace(current.String()[5:])
			var js json.RawMessage
			if json.Unmarshal([]byte(payload), &js) == nil {
				events = append(events, current.String())
				current.Reset()
				inData = false
			} else {
				current.WriteString("\n")
				current.WriteString(line)
			}
		} else if inData && strings.TrimSpace(line) == "" {
			if current.Len() > 0 {
				events = append(events, current.String())
				current.Reset()
			}
			inData = false
		}
	}
	if current.Len() > 0 {
		events = append(events, current.String())
	}

	var cleaned []string
	for _, e := range events {
		e = strings.TrimSpace(e)
		if strings.HasPrefix(e, "data:") {
			payload := strings.TrimSpace(e[5:])
			joined := strings.ReplaceAll(payload, "\n", "")
			cleaned = append(cleaned, fmt.Sprintf("data: %s", joined))
		}
	}
	return cleaned
}
