package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

func FlattenContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
				} else if c, ok := m["content"].(string); ok {
					sb.WriteString(c)
				}
			} else {
				sb.WriteString(fmt.Sprint(item))
			}
		}
		return sb.String()
	default:
		return fmt.Sprint(content)
	}
}

func ConvertContent(content interface{}) interface{} {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		hasImage := false
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "image_url" {
					hasImage = true
					break
				}
			}
		}

		if !hasImage {
			var sb strings.Builder
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if t, ok := m["text"].(string); ok {
						sb.WriteString(t)
					} else if c, ok := m["content"].(string); ok {
						sb.WriteString(c)
					}
				} else {
					sb.WriteString(fmt.Sprint(item))
				}
			}
			return sb.String()
		}

		var blocks []map[string]interface{}
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				blocks = append(blocks, map[string]interface{}{
					"type": "text",
					"text": fmt.Sprint(item),
				})
				continue
			}
			switch m["type"] {
			case "text":
				text, _ := m["text"].(string)
				blocks = append(blocks, map[string]interface{}{
					"type": "text",
					"text": text,
				})
			case "image_url":
				url := extractImageURL(m)
				if strings.HasPrefix(url, "data:") {
					parts := strings.SplitN(url, ",", 2)
					if len(parts) == 2 {
						header := parts[0]
						data := parts[1]
						mediaType := ""
						headerParts := strings.SplitN(header, ";", 2)
						if len(headerParts) >= 1 {
							colonParts := strings.SplitN(headerParts[0], ":", 2)
							if len(colonParts) == 2 {
								mediaType = colonParts[1]
							}
						}
						blocks = append(blocks, map[string]interface{}{
							"type": "image",
							"source": map[string]interface{}{
								"type":       "base64",
								"media_type": mediaType,
								"data":       data,
							},
						})
					}
				} else {
					blocks = append(blocks, map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type": "url",
							"url":  url,
						},
					})
				}
			}
		}
		return blocks
	default:
		return fmt.Sprint(content)
	}
}

func extractImageURL(m map[string]interface{}) string {
	imageURL := m["image_url"]
	switch v := imageURL.(type) {
	case map[string]interface{}:
		if u, ok := v["url"].(string); ok {
			return u
		}
	case string:
		return v
	}
	return ""
}

func ConvertMessages(messages []interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	i := 0
	for i < len(messages) {
		m, ok := messages[i].(map[string]interface{})
		if !ok {
			i++
			continue
		}
		role, _ := m["role"].(string)

		if role == "system" {
			i++
			continue
		}

		if role == "tool" {
			var blocks []map[string]interface{}
			for i < len(messages) {
				tm, ok := messages[i].(map[string]interface{})
				if !ok {
					break
				}
				if r, _ := tm["role"].(string); r != "tool" {
					break
				}
				toolCallID, _ := tm["tool_call_id"].(string)
				blocks = append(blocks, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": toolCallID,
					"content":     FlattenContent(tm["content"]),
				})
				i++
			}
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": blocks,
			})
			continue
		}

		if role == "assistant" {
			toolCalls, hasToolCalls := m["tool_calls"].([]interface{})
			if hasToolCalls && len(toolCalls) > 0 {
				var blocks []interface{}
				if content := m["content"]; content != nil {
					text := FlattenContent(content)
					if text != "" {
						blocks = append(blocks, map[string]interface{}{
							"type": "text",
							"text": text,
						})
					}
				}
				for _, tc := range toolCalls {
					tcMap, ok := tc.(map[string]interface{})
					if !ok {
						continue
					}
					f, _ := tcMap["function"].(map[string]interface{})
					argsStr, _ := f["arguments"].(string)
					var args interface{}
					if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
						args = map[string]interface{}{}
					}
					id, _ := tcMap["id"].(string)
					if id == "" {
						id = "toolu_" + ShortID()
					}
					name, _ := f["name"].(string)
					blocks = append(blocks, map[string]interface{}{
						"type":  "tool_use",
						"id":    id,
						"name":  name,
						"input": args,
					})
				}
				result = append(result, map[string]interface{}{
					"role":    "assistant",
					"content": blocks,
				})
				i++
				continue
			}
			result = append(result, map[string]interface{}{
				"role":    "assistant",
				"content": ConvertContent(m["content"]),
			})
			i++
			continue
		}

		result = append(result, map[string]interface{}{
			"role":    "user",
			"content": ConvertContent(m["content"]),
		})
		i++
	}
	return result
}

func ConvertTools(tools []interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	for _, t := range tools {
		tMap, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		if tMap["type"] == "function" {
			f, _ := tMap["function"].(map[string]interface{})
			name, _ := f["name"].(string)
			desc, _ := f["description"].(string)
			params := f["parameters"]
			if params == nil {
				params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			result = append(result, map[string]interface{}{
				"name":         name,
				"description":  desc,
				"input_schema": params,
			})
		} else if _, hasName := tMap["name"]; hasName {
			if _, hasSchema := tMap["input_schema"]; hasSchema {
				result = append(result, tMap)
			} else if _, hasDesc := tMap["description"]; hasDesc {
				result = append(result, tMap)
			}
		}
	}
	return result
}

func ConvertToolChoice(tc interface{}) map[string]interface{} {
	switch v := tc.(type) {
	case string:
		switch v {
		case "required":
			return map[string]interface{}{"type": "any"}
		case "none":
			return map[string]interface{}{"type": "none"}
		default:
			return map[string]interface{}{"type": "auto"}
		}
	case map[string]interface{}:
		if v["type"] == "function" {
			f, _ := v["function"].(map[string]interface{})
			name, _ := f["name"].(string)
			return map[string]interface{}{"type": "tool", "name": name}
		}
	}
	return map[string]interface{}{"type": "auto"}
}
