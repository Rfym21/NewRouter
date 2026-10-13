package anonymizer

import (
	"strings"

	"new-router/common"
)

// AnonymizeChatRequest anonymizes OpenAI chat-compatible request JSON.
func AnonymizeChatRequest(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return data, err
	}
	messages, ok := body["messages"].([]any)
	if !ok {
		return data, nil
	}
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "user", "tool", "system", "developer":
			msg["content"] = anonymizeContent(msg["content"], session)
		}
	}
	return common.Marshal(body)
}

// AnonymizeEmbeddingRequest anonymizes text inputs in an embeddings request.
func AnonymizeEmbeddingRequest(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return data, err
	}
	if input, ok := body["input"]; ok {
		body["input"] = anonymizeEmbeddingInput(input, session)
	}
	return common.Marshal(body)
}

// AnonymizeResponsesRequest anonymizes text-bearing fields in a Responses request.
func AnonymizeResponsesRequest(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return data, err
	}
	if instructions, ok := body["instructions"]; ok {
		body["instructions"] = anonymizeResponseItem(instructions, session, false)
	}
	if input, ok := body["input"]; ok {
		body["input"] = anonymizeResponseItem(input, session, true)
	}
	return common.Marshal(body)
}

// AnonymizeTextFields anonymizes common text-bearing JSON fields in any request shape.
func AnonymizeTextFields(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var value any
	if err := common.UnmarshalWithNumber(data, &value); err != nil {
		return data, err
	}
	value = transformTextFields(value, session.AnonymizeString, requestTextKeys)
	return common.Marshal(value)
}

// DeanonymizeChatResponse restores chat response text and tool-call arguments.
func DeanonymizeChatResponse(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return data, err
	}
	deanonymizeChatChoices(body, session)
	return common.Marshal(body)
}

// DeanonymizeResponsesResponse restores Responses API text and tool arguments.
func DeanonymizeResponsesResponse(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var body map[string]any
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return data, err
	}
	body = transformTextFields(body, session.DeanonymizeString, responseTextKeys).(map[string]any)
	return common.Marshal(body)
}

// DeanonymizeTextFields restores common text-bearing JSON fields in any response shape.
func DeanonymizeTextFields(data []byte, session *Session) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var value any
	if err := common.UnmarshalWithNumber(data, &value); err != nil {
		return data, err
	}
	value = transformTextFields(value, session.DeanonymizeString, responseTextKeys)
	return common.Marshal(value)
}

func anonymizeContent(value any, session *Session) any {
	switch v := value.(type) {
	case string:
		return session.AnonymizeString(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				out[i] = item
				continue
			}
			part = cloneMap(part)
			partType, _ := part["type"].(string)
			if (partType == "text" || partType == "input_text" || partType == "output_text") && isString(part["text"]) {
				part["text"] = session.AnonymizeString(part["text"].(string))
			}
			out[i] = part
		}
		return out
	default:
		return value
	}
}

func anonymizeEmbeddingInput(value any, session *Session) any {
	switch v := value.(type) {
	case string:
		return session.AnonymizeString(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			if str, ok := item.(string); ok {
				out[i] = session.AnonymizeString(str)
			} else {
				out[i] = item
			}
		}
		return out
	default:
		return value
	}
}

func anonymizeResponseItem(value any, session *Session, isToolOutput bool) any {
	switch v := value.(type) {
	case string:
		return session.AnonymizeString(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = anonymizeResponseItem(item, session, isToolOutput)
		}
		return out
	case map[string]any:
		out := cloneMap(v)
		role, _ := out["role"].(string)
		if role == "system" || role == "developer" {
			isToolOutput = false
		}
		if content, ok := out["content"]; ok {
			out["content"] = anonymizeResponseContent(content, session, isToolOutput)
		}
		if out["type"] == "function_call_output" {
			if output, ok := out["output"].(string); ok {
				out["output"] = session.AnonymizeString(output)
			}
		}
		return out
	default:
		return value
	}
}

func anonymizeResponseContent(value any, session *Session, isToolOutput bool) any {
	switch v := value.(type) {
	case string:
		return session.AnonymizeString(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			part, ok := item.(map[string]any)
			if !ok {
				out[i] = item
				continue
			}
			part = cloneMap(part)
			if text, ok := part["text"].(string); ok {
				part["text"] = session.AnonymizeString(text)
			}
			out[i] = part
		}
		return out
	default:
		return value
	}
}

func deanonymizeChatChoices(body map[string]any, session *Session) {
	choices, ok := body["choices"].([]any)
	if !ok {
		return
	}
	for _, item := range choices {
		choice, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if message, ok := choice["message"].(map[string]any); ok {
			deanonymizeChatMessage(message, session)
		}
		if delta, ok := choice["delta"].(map[string]any); ok {
			deanonymizeChatMessage(delta, session)
		}
	}
}

func deanonymizeChatMessage(message map[string]any, session *Session) {
	if content, ok := message["content"].(string); ok {
		message["content"] = session.DeanonymizeString(content)
	}
	if reasoning, ok := message["reasoning_content"].(string); ok {
		message["reasoning_content"] = session.DeanonymizeString(reasoning)
	}
	if reasoning, ok := message["reasoning"].(string); ok {
		message["reasoning"] = session.DeanonymizeString(reasoning)
	}
	toolCalls, ok := message["tool_calls"].([]any)
	if !ok {
		return
	}
	for _, item := range toolCalls {
		toolCall, ok := item.(map[string]any)
		if !ok {
			continue
		}
		function, ok := toolCall["function"].(map[string]any)
		if !ok {
			continue
		}
		function["arguments"] = deanonymizeJSONStringOrValue(function["arguments"], session)
	}
}

func deanonymizeJSONStringOrValue(value any, session *Session) any {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return v
		}
		var parsed any
		if err := common.UnmarshalWithNumber([]byte(trimmed), &parsed); err == nil {
			parsed = session.DeanonymizeValue(parsed)
			if encoded, err := common.Marshal(parsed); err == nil {
				return string(encoded)
			}
		}
		return session.DeanonymizeString(v)
	case map[string]any, []any:
		restored := session.DeanonymizeValue(v)
		if encoded, err := common.Marshal(restored); err == nil {
			return string(encoded)
		}
		return restored
	default:
		return value
	}
}

var requestTextKeys = map[string]struct{}{
	"content": {}, "text": {}, "input": {}, "prompt": {}, "instructions": {},
	"output": {}, "arguments": {},
}

var responseTextKeys = map[string]struct{}{
	"content": {}, "text": {}, "output_text": {}, "delta": {}, "arguments": {}, "output": {},
	"reasoning": {}, "reasoning_content": {},
}

func transformTextFields(value any, transform func(string) string, keys map[string]struct{}) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = transformTextFields(item, transform, keys)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			lowerKey := strings.ToLower(key)
			if str, ok := item.(string); ok {
				if _, shouldTransform := keys[lowerKey]; shouldTransform {
					out[key] = transform(str)
					continue
				}
			}
			if lowerKey == "arguments" || lowerKey == "input" {
				out[key] = transformJSONArgument(item, transform)
				continue
			}
			out[key] = transformTextFields(item, transform, keys)
		}
		return out
	default:
		return value
	}
}

func transformJSONArgument(value any, transform func(string) string) any {
	switch v := value.(type) {
	case string:
		var parsed any
		if err := common.UnmarshalWithNumber([]byte(strings.TrimSpace(v)), &parsed); err == nil {
			parsed = transformAllStrings(parsed, transform)
			if encoded, err := common.Marshal(parsed); err == nil {
				return string(encoded)
			}
		}
		return transform(v)
	case map[string]any, []any:
		return transformAllStrings(v, transform)
	default:
		return value
	}
}

func transformAllStrings(value any, transform func(string) string) any {
	switch v := value.(type) {
	case string:
		return transform(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = transformAllStrings(item, transform)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = transformAllStrings(item, transform)
		}
		return out
	default:
		return value
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func isString(value any) bool {
	_, ok := value.(string)
	return ok
}
