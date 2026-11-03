package openai

import (
	"sort"
	"strconv"
	"strings"

	"new-router/common"
	"new-router/dto"
	"new-router/logger"
	"new-router/pkg/anonymizer"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Typed conversion paths share the raw protocol transformer so legacy
// function_call, parallel tool calls, and finish-time tails behave identically.
func deanonymizeChatStreamChunk(c *gin.Context, chunk *dto.ChatCompletionsStreamResponse, restorer *anonymizer.StreamRestorer) {
	if restorer == nil || chunk == nil {
		return
	}
	data, err := common.Marshal(chunk)
	if err != nil {
		return
	}
	restored, err := deanonymizeStreamChunk(c, string(data), restorer)
	if err == nil {
		_ = common.UnmarshalJsonStr(restored, chunk)
	}
}

func deanonymizeResponseBytes(c *gin.Context, data []byte) ([]byte, error) {
	session := anonymizer.FromContext(c)
	if session == nil {
		return data, nil
	}
	restored, err := anonymizer.DeanonymizeTextFields(data, session)
	if err != nil {
		if common.DebugEnabled {
			logger.LogDebug(c, "deanonymize response skipped (unparseable): %s", err.Error())
		}
		return data, nil
	}
	return restored, nil
}

func deanonymizeResponseString(c *gin.Context, data string) (string, error) {
	if data == "" {
		return data, nil
	}
	out, err := deanonymizeResponseBytes(c, []byte(data))
	return string(out), err
}

var streamDeltaTextFields = []string{"content", "refusal", "reasoning_content", "reasoning"}

// deanonymizeStreamChunk edits only text/argument slots. Accounting fields and
// unknown extensions retain their original JSON bytes. Argument fragments have
// their inner JSON decoded/restored/re-escaped before the outer SSE serialization.
func deanonymizeStreamChunk(c *gin.Context, data string, restorer *anonymizer.StreamRestorer) (string, error) {
	if restorer == nil {
		return deanonymizeResponseString(c, data)
	}
	if anonymizer.FromContext(c) == nil {
		return data, nil
	}
	choices := gjson.Get(data, "choices")
	if !choices.IsArray() {
		return deanonymizeResponseString(c, data)
	}
	out := data
	for pos, choice := range choices.Array() {
		logicalIdx := pos
		if index := choice.Get("index"); index.Exists() {
			logicalIdx = int(index.Int())
		}
		base := "choices." + strconv.Itoa(pos)
		key := strconv.Itoa(logicalIdx)
		set := func(path, value string) error {
			patched, err := sjson.Set(out, path, value)
			if err == nil {
				out = patched
			}
			return err
		}
		if text := choice.Get("text"); text.Type == gjson.String {
			if err := set(base+".text", restorer.Push(key+":text", text.String())); err != nil {
				return data, err
			}
		}
		for _, field := range streamDeltaTextFields {
			value := choice.Get("delta." + field)
			if value.Type == gjson.String {
				if err := set(base+".delta."+field, restorer.Push(key+":"+field, value.String())); err != nil {
					return data, err
				}
			}
		}
		if args := choice.Get("delta.function_call.arguments"); args.Type == gjson.String {
			if err := set(base+".delta.function_call.arguments", restorer.PushJSON(key+":function_call", args.String())); err != nil {
				return data, err
			}
		}
		for toolPos, tool := range choice.Get("delta.tool_calls").Array() {
			args := tool.Get("function.arguments")
			if args.Type != gjson.String {
				continue
			}
			toolIdx := toolPos
			if index := tool.Get("index"); index.Exists() {
				toolIdx = int(index.Int())
			}
			path := base + ".delta.tool_calls." + strconv.Itoa(toolPos) + ".function.arguments"
			if err := set(path, restorer.PushJSON(key+":tool:"+strconv.Itoa(toolIdx), args.String())); err != nil {
				return data, err
			}
		}
		if finish := choice.Get("finish_reason"); finish.Type == gjson.String && finish.String() != "" {
			// Append held tails to this choice's finish frame so no restored
			// content can appear after its finish_reason.
			for _, field := range append([]string{"text"}, streamDeltaTextFields...) {
				path := base + ".delta." + field
				if field == "text" {
					path = base + ".text"
				}
				if tail := restorer.Flush(key + ":" + field); tail != "" {
					if err := set(path, gjson.Get(out, path).String()+tail); err != nil {
						return data, err
					}
				}
			}
			for _, jsonKey := range restorer.PendingJSONKeys() {
				if !strings.HasPrefix(jsonKey, key+":") {
					continue
				}
				tail := restorer.FlushJSON(jsonKey)
				if tail == "" {
					continue
				}
				path := base + ".delta.function_call.arguments"
				if toolIndex, ok := strings.CutPrefix(jsonKey, key+":tool:"); ok {
					index, _ := strconv.Atoi(toolIndex)
					toolPosition := -1
					for candidatePos, candidate := range gjson.Get(out, base+".delta.tool_calls").Array() {
						candidateIndex := candidatePos
						if v := candidate.Get("index"); v.Exists() {
							candidateIndex = int(v.Int())
						}
						if candidateIndex == index {
							toolPosition = candidatePos
							break
						}
					}
					if toolPosition < 0 {
						toolPosition = len(gjson.Get(out, base+".delta.tool_calls").Array())
						patched, err := sjson.Set(out, base+".delta.tool_calls."+strconv.Itoa(toolPosition)+".index", index)
						if err != nil {
							return data, err
						}
						out = patched
					}
					path = base + ".delta.tool_calls." + strconv.Itoa(toolPosition) + ".function.arguments"
				}
				if err := set(path, gjson.Get(out, path).String()+tail); err != nil {
					return data, err
				}
			}
		}
	}
	return out, nil
}

// chatStreamResidualChoices is the abnormal-EOF safety net for all converted
// Chat paths. Normal finish frames have already drained these buffers.
func chatStreamResidualChoices(restorer *anonymizer.StreamRestorer) []dto.ChatCompletionsStreamResponseChoice {
	var choices []dto.ChatCompletionsStreamResponseChoice
	keys := restorer.PendingKeys()
	sort.Strings(keys)
	for _, key := range keys {
		idx, field, ok := chatStreamKey(key)
		if !ok {
			continue
		}
		tail := restorer.Flush(key)
		if tail == "" {
			continue
		}
		choice := dto.ChatCompletionsStreamResponseChoice{Index: idx}
		switch field {
		case "text":
			choice.Text = &tail
		case "content":
			choice.Delta.Content = &tail
		case "refusal":
			choice.Delta.Refusal = &tail
		case "reasoning_content":
			choice.Delta.ReasoningContent = &tail
		case "reasoning":
			choice.Delta.Reasoning = &tail
		default:
			continue
		}
		choices = append(choices, choice)
	}
	keys = restorer.PendingJSONKeys()
	sort.Strings(keys)
	for _, key := range keys {
		idx, field, ok := chatStreamKey(key)
		if !ok {
			continue
		}
		tail := restorer.FlushJSON(key)
		if tail == "" {
			continue
		}
		choice := dto.ChatCompletionsStreamResponseChoice{Index: idx}
		if field == "function_call" {
			choice.Delta.FunctionCall = &dto.FunctionResponse{Arguments: tail}
		} else if toolIndex, ok := strings.CutPrefix(field, "tool:"); ok {
			index, _ := strconv.Atoi(toolIndex)
			choice.Delta.ToolCalls = []dto.ToolCallResponse{{Index: &index, Function: dto.FunctionResponse{Arguments: tail}}}
		} else {
			continue
		}
		choices = append(choices, choice)
	}
	return choices
}

func chatStreamKey(key string) (int, string, bool) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	idx, err := strconv.Atoi(parts[0])
	return idx, parts[1], err == nil
}
