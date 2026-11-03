package anonymizer

import (
	"encoding/json"
	"errors"
	"strings"

	"new-router/common"
)

// Request transforms operate on protocol text slots. Identifiers, tool schemas,
// metadata and media are retained even when their strings look like PII.
func transformRequest(data []byte, session *Session, transform func(map[string]any)) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	var body map[string]any
	if !json.Valid(data) {
		return data, errors.New("invalid anonymization request JSON")
	}
	if err := common.UnmarshalWithNumber(data, &body); err != nil {
		return data, err
	}
	if body == nil {
		return data, errors.New("invalid anonymization request object")
	}
	success := false
	session.beginRequestPreflight()
	defer func() { session.endRequestTransform(success) }()
	transform(body)
	if err := session.Err(); err != nil {
		return data, err
	}
	session.preflighting = false
	transform(body)
	if err := session.Err(); err != nil {
		return data, err
	}
	encoded, err := common.Marshal(body)
	success = err == nil
	return encoded, err
}

// AnonymizeCompletionRequest covers the legacy /v1/completions text prompt.
// Integer token arrays are intentionally left intact.
func AnonymizeCompletionRequest(data []byte, session *Session) ([]byte, error) {
	return transformRequest(data, session, func(body map[string]any) {
		if prompt, ok := body["prompt"]; ok {
			body["prompt"] = transformTextInput(prompt, session.AnonymizeString)
		}
		if suffix, ok := body["suffix"].(string); ok {
			body["suffix"] = session.AnonymizeString(suffix)
		}
	})
}

// AnonymizeChatRequest includes assistant history and both generations of tool
// calls, so a subsequent turn cannot send previously restored private text.
func AnonymizeChatRequest(data []byte, session *Session) ([]byte, error) {
	return transformRequest(data, session, func(body map[string]any) {
		if messages, ok := body["messages"].([]any); ok {
			for _, item := range messages {
				if msg, ok := item.(map[string]any); ok {
					transformChatMessage(msg, session.AnonymizeString)
				}
			}
		}
	})
}

func transformChatMessage(msg map[string]any, transform func(string) string) {
	if protectedTextObject(msg) {
		return
	}
	if content, ok := msg["content"]; ok {
		msg["content"] = transformContent(content, transform)
	}
	for _, key := range []string{"refusal", "reasoning", "reasoning_content"} {
		if text, ok := msg[key].(string); ok {
			msg[key] = transform(text)
		}
	}
	if call, ok := msg["function_call"].(map[string]any); ok {
		transformFunctionArguments(call, transform)
	}
	if calls, ok := msg["tool_calls"].([]any); ok {
		for _, item := range calls {
			if call, ok := item.(map[string]any); ok {
				if function, ok := call["function"].(map[string]any); ok {
					transformFunctionArguments(function, transform)
				}
			}
		}
	}
}

func transformFunctionArguments(call map[string]any, transform func(string) string) {
	if arguments, ok := call["arguments"]; ok {
		call["arguments"] = transformJSONArgument(arguments, transform)
	}
}

func AnonymizeEmbeddingRequest(data []byte, session *Session) ([]byte, error) {
	return transformRequest(data, session, func(body map[string]any) {
		if input, ok := body["input"]; ok {
			body["input"] = transformTextInput(input, session.AnonymizeString)
		}
	})
}

// AnonymizeResponsesRequest handles Responses and compact input with the same
// contract. Compaction ciphertext is opaque and must retain its exact value.
func AnonymizeResponsesRequest(data []byte, session *Session) ([]byte, error) {
	return transformRequest(data, session, func(body map[string]any) {
		if instructions, ok := body["instructions"]; ok {
			body["instructions"] = transformContent(instructions, session.AnonymizeString)
		}
		if input, ok := body["input"]; ok {
			body["input"] = transformResponseInput(input, session.AnonymizeString)
		}
	})
}

func transformResponseInput(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case string:
		return transform(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = transformResponseInput(item, transform)
		}
		return out
	case map[string]any:
		if protectedTextObject(typed) {
			return typed
		}
		out := cloneMap(typed)
		if content, ok := out["content"]; ok {
			out["content"] = transformContent(content, transform)
		}
		if text, ok := out["text"].(string); ok {
			out["text"] = transform(text)
		}
		if summary, ok := out["summary"]; ok {
			out["summary"] = transformContent(summary, transform)
		}
		switch out["type"] {
		case "function_call":
			transformFunctionArguments(out, transform)
		case "custom_tool_call":
			if input, ok := out["input"]; ok {
				out["input"] = transformJSONArgument(input, transform)
			}
		case "function_call_output", "custom_tool_call_output":
			if output, ok := out["output"]; ok {
				out["output"] = transformToolResult(output, transform)
			}
		}
		return out
	default:
		return value
	}
}

// AnonymizeClaudeRequest covers native Messages text, tool inputs and results.
// Thinking blocks are preserved because their signature can arrive later.
func AnonymizeClaudeRequest(data []byte, session *Session) ([]byte, error) {
	return transformRequest(data, session, func(body map[string]any) {
		if system, ok := body["system"]; ok {
			body["system"] = transformContent(system, session.AnonymizeString)
		}
		if messages, ok := body["messages"].([]any); ok {
			for _, item := range messages {
				if message, ok := item.(map[string]any); ok {
					if content, ok := message["content"]; ok {
						message["content"] = transformContent(content, session.AnonymizeString)
					}
				}
			}
		}
	})
}

// AnonymizeGeminiRequest handles text and native function payloads without
// changing inlineData/fileData or signed thought parts.
func AnonymizeGeminiRequest(data []byte, session *Session) ([]byte, error) {
	return transformRequest(data, session, func(body map[string]any) {
		if content, ok := body["content"].(map[string]any); ok {
			transformGeminiContent(content, session.AnonymizeString)
		}
		if requests, ok := body["requests"].([]any); ok {
			for _, item := range requests {
				if request, ok := item.(map[string]any); ok {
					if content, ok := request["content"].(map[string]any); ok {
						transformGeminiContent(content, session.AnonymizeString)
					}
				}
			}
		}
		for _, key := range []string{"systemInstruction", "system_instruction"} {
			if instruction, ok := body[key].(map[string]any); ok {
				transformGeminiContent(instruction, session.AnonymizeString)
			}
		}
		if contents, ok := body["contents"].([]any); ok {
			for _, item := range contents {
				if content, ok := item.(map[string]any); ok {
					transformGeminiContent(content, session.AnonymizeString)
				}
			}
		}
	})
}

func transformGeminiContent(content map[string]any, transform func(string) string) {
	parts, ok := content["parts"].([]any)
	if !ok {
		return
	}
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok || protectedTextObject(part) {
			continue
		}
		if text, ok := part["text"].(string); ok {
			part["text"] = transform(text)
		}
		for _, key := range []string{"functionCall", "function_call"} {
			if call, ok := part[key].(map[string]any); ok {
				if args, ok := call["args"]; ok {
					call["args"] = transformAllStrings(args, transform)
				}
			}
		}
		for _, key := range []string{"functionResponse", "function_response"} {
			if response, ok := part[key].(map[string]any); ok {
				if result, ok := response["response"]; ok {
					response["response"] = transformAllStrings(result, transform)
				}
			}
		}
	}
}

func transformContent(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case string:
		return transform(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = transformContent(item, transform)
		}
		return out
	case map[string]any:
		if protectedTextObject(typed) {
			return typed
		}
		out := cloneMap(typed)
		switch out["type"] {
		case "text", "input_text", "output_text", "summary_text", "reasoning_text", "reasoning_summary_text":
			if text, ok := out["text"].(string); ok {
				out["text"] = transform(text)
			}
		case "refusal":
			if text, ok := out["refusal"].(string); ok {
				out["refusal"] = transform(text)
			}
		case "tool_use", "server_tool_use":
			if input, ok := out["input"]; ok {
				out["input"] = transformAllStrings(input, transform)
			}
		case "tool_result":
			if content, ok := out["content"]; ok {
				out["content"] = transformToolResult(content, transform)
			}
		case "compaction":
			if content, ok := out["content"].(string); ok {
				out["content"] = transform(content)
			}
		}
		return out
	default:
		return value
	}
}

func transformTextInput(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case string:
		return transform(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			if text, ok := item.(string); ok {
				out[i] = transform(text)
			} else {
				out[i] = item
			}
		}
		return out
	default:
		return value
	}
}

func transformToolResult(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case string:
		return transformJSONArgument(typed, transform)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			if block, ok := item.(map[string]any); ok && (protectedTextObject(block) || mediaObject(block)) {
				out[i] = item
			} else if block, ok := item.(map[string]any); ok && (block["type"] == "text" || block["type"] == "input_text" || block["type"] == "output_text") {
				out[i] = transformContent(block, transform)
			} else {
				out[i] = transformAllStrings(item, transform)
			}
		}
		return out
	case map[string]any:
		return transformAllStrings(typed, transform)
	default:
		return value
	}
}

// AnonymizeTextFields is a conservative fallback for compatible JSON protocols.
func AnonymizeTextFields(data []byte, session *Session) ([]byte, error) {
	return transformJSONText(data, session, sessionTransform(session, false), requestTextKeys, true)
}

func sessionTransform(session *Session, restore bool) func(string) string {
	if session == nil {
		return func(text string) string { return text }
	}
	if restore {
		return session.DeanonymizeString
	}
	return session.AnonymizeString
}

func DeanonymizeChatResponse(data []byte, session *Session) ([]byte, error) {
	return DeanonymizeTextFields(data, session)
}

func DeanonymizeResponsesResponse(data []byte, session *Session) ([]byte, error) {
	return DeanonymizeTextFields(data, session)
}

func DeanonymizeTextFields(data []byte, session *Session) ([]byte, error) {
	return transformJSONText(data, session, sessionTransform(session, true), responseTextKeys, false)
}

func transformJSONText(data []byte, session *Session, transform func(string) string, keys map[string]struct{}, anonymize bool) ([]byte, error) {
	if session == nil || len(data) == 0 {
		return data, nil
	}
	if !json.Valid(data) {
		return data, errors.New("invalid anonymization JSON")
	}
	var value any
	if err := common.UnmarshalWithNumber(data, &value); err != nil {
		return data, err
	}
	success := false
	if anonymize {
		session.beginRequestPreflight()
		defer func() { session.endRequestTransform(success) }()
		value = transformTextFields(value, transform, keys)
		if err := session.Err(); err != nil {
			return data, err
		}
		session.preflighting = false
	}
	value = transformTextFields(value, transform, keys)
	if err := session.Err(); err != nil {
		return data, err
	}
	encoded, err := common.Marshal(value)
	success = err == nil
	return encoded, err
}

var requestTextKeys = map[string]struct{}{
	"content": {}, "text": {}, "input": {}, "prompt": {}, "instructions": {},
	"output": {}, "arguments": {}, "refusal": {}, "reasoning": {}, "reasoning_content": {},
}
var responseTextKeys = map[string]struct{}{
	"content": {}, "text": {}, "output_text": {}, "delta": {}, "arguments": {}, "output": {}, "input": {},
	"reasoning": {}, "reasoning_content": {}, "refusal": {},
}
var structuralTextKeys = map[string]struct{}{
	"metadata": {}, "tools": {}, "functions": {}, "tool_choice": {}, "function_call": {},
	"response_format": {}, "format": {}, "schema": {}, "parameters": {}, "input_schema": {},
	"properties": {}, "headers": {}, "source": {}, "image_url": {}, "audio": {}, "input_audio": {},
	"inlineData": {}, "inline_data": {}, "fileData": {}, "file_data": {},
	"encrypted_content": {}, "signature": {}, "thoughtSignature": {}, "thought_signature": {},
}

func transformTextFields(value any, transform func(string) string, keys map[string]struct{}) any {
	switch typed := value.(type) {
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = transformTextFields(item, transform, keys)
		}
		return out
	case map[string]any:
		if protectedTextObject(typed) || mediaObject(typed) {
			return typed
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(key)
			if lower == "function_call" {
				if call, ok := item.(map[string]any); ok {
					function := cloneMap(call)
					transformFunctionArguments(function, transform)
					if args, ok := function["args"]; ok {
						function["args"] = transformAllStrings(args, transform)
					}
					out[key] = function
				} else {
					out[key] = item
				}
				continue
			}
			if _, structural := structuralTextKeys[key]; structural {
				out[key] = item
				continue
			}
			// JSON arguments must be decoded before restoring quotes, backslashes or
			// newlines; raw replacement would invalidate the nested JSON string.
			if lower == "arguments" || (lower == "input" && (typed["type"] == "tool_use" || typed["type"] == "server_tool_use" || typed["type"] == "custom_tool_call")) {
				out[key] = transformJSONArgument(item, transform)
				continue
			}
			if lower == "output" && (typed["type"] == "function_call_output" || typed["type"] == "custom_tool_call_output") {
				out[key] = transformToolResult(item, transform)
				continue
			}
			if lower == "functioncall" {
				if call, ok := item.(map[string]any); ok {
					function := cloneMap(call)
					if args, ok := function["args"]; ok {
						function["args"] = transformAllStrings(args, transform)
					}
					out[key] = function
				} else {
					out[key] = item
				}
				continue
			}
			if lower == "functionresponse" || lower == "function_response" {
				if result, ok := item.(map[string]any); ok {
					function := cloneMap(result)
					if content, ok := function["response"]; ok {
						function["response"] = transformAllStrings(content, transform)
					}
					out[key] = function
				} else {
					out[key] = item
				}
				continue
			}
			if text, ok := item.(string); ok {
				if _, known := keys[lower]; known {
					out[key] = transform(text)
					continue
				}
			}
			if lower == "content" && typed["type"] == "tool_result" {
				out[key] = transformToolResult(item, transform)
				continue
			}
			if lower == "prompt" || lower == "input" {
				if items, ok := item.([]any); ok {
					textItems := make([]any, len(items))
					for i, value := range items {
						if text, ok := value.(string); ok {
							textItems[i] = transform(text)
						} else {
							textItems[i] = transformTextFields(value, transform, keys)
						}
					}
					out[key] = textItems
					continue
				}
			}
			out[key] = transformTextFields(item, transform, keys)
		}
		return out
	default:
		return value
	}
}

func protectedTextObject(value map[string]any) bool {
	switch value["type"] {
	case "thinking", "redacted_thinking", "thinking_delta", "signature_delta":
		return true
	case "compaction":
		if blob, ok := value["encrypted_content"].(string); ok && blob != "" {
			return true
		}
	}
	for _, key := range []string{"signature", "thoughtSignature", "thought_signature"} {
		if signature, ok := value[key].(string); ok && signature != "" {
			return true
		}
	}
	return false
}

func mediaObject(value map[string]any) bool {
	switch value["type"] {
	case "image", "image_url", "input_image", "output_image", "input_audio", "audio", "document", "file", "input_file", "output_file", "video":
		return true
	}
	return false
}

func transformJSONArgument(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		var parsed any
		if json.Valid([]byte(trimmed)) && common.UnmarshalWithNumber([]byte(trimmed), &parsed) == nil {
			parsed = transformAllStrings(parsed, transform)
			if encoded, err := common.Marshal(parsed); err == nil {
				return string(encoded)
			}
		}
		return transform(typed)
	case map[string]any, []any:
		return transformAllStrings(typed, transform)
	default:
		return value
	}
}

func transformAllStrings(value any, transform func(string) string) any {
	switch typed := value.(type) {
	case string:
		return transform(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = transformAllStrings(item, transform)
		}
		return out
	case map[string]any:
		// These are business JSON values inside a tool payload, not protocol
		// envelopes. Keys such as metadata/signature/type carry ordinary data.
		out := make(map[string]any, len(typed))
		for key, item := range typed {
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
