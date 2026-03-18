package controller

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/songquanpeng/one-api/common"
	"github.com/songquanpeng/one-api/common/helper"
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/common/render"
	relayrelay "github.com/songquanpeng/one-api/relay"
	anthropicAdaptor "github.com/songquanpeng/one-api/relay/adaptor/anthropic"
	"github.com/songquanpeng/one-api/relay/adaptor/openai"
	"github.com/songquanpeng/one-api/relay/billing"
	billingratio "github.com/songquanpeng/one-api/relay/billing/ratio"
	"github.com/songquanpeng/one-api/relay/meta"
	"github.com/songquanpeng/one-api/relay/model"
	"github.com/songquanpeng/one-api/relay/relaymode"
)

// parseAnthropicSystem 解析 system 字段，支持字符串和数组两种格式
func parseAnthropicSystem(system any) string {
	if system == nil {
		return ""
	}
	switch v := system.(type) {
	case string:
		return v
	case []any:
		// [{"type":"text","text":"..."}]
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// anthropicNativeRequestToOpenAI 将 Anthropic 原生请求转为 OpenAI 通用格式
// 支持 text / image / tool_use / tool_result 四种 content block 类型
func anthropicNativeRequestToOpenAI(req *anthropicAdaptor.Request) *model.GeneralOpenAIRequest {
	openaiReq := &model.GeneralOpenAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	// system prompt
	if systemText := parseAnthropicSystem(req.System); systemText != "" {
		openaiReq.Messages = append(openaiReq.Messages, model.Message{
			Role:    "system",
			Content: systemText,
		})
	}

	// messages
	for _, msg := range req.Messages {
		contents := msg.ParseContents()

		// ── 情形 A: assistant 消息，可能混合 text + tool_use ──────────────
		if msg.Role == "assistant" {
			var textParts []string
			var toolCalls []model.Tool
			for _, c := range contents {
				switch c.Type {
				case "text":
					if c.Text != "" {
						textParts = append(textParts, c.Text)
					}
				case "tool_use":
					// Anthropic tool_use → OpenAI tool_call
					argsBytes, _ := json.Marshal(c.Input)
					toolCalls = append(toolCalls, model.Tool{
						Id:   c.Id,
						Type: "function",
						Function: model.Function{
							Name:      c.Name,
							Arguments: string(argsBytes),
						},
					})
				}
			}
			openaiMsg := model.Message{Role: "assistant"}
			if len(textParts) > 0 {
				openaiMsg.Content = strings.Join(textParts, "")
			}
			if len(toolCalls) > 0 {
				openaiMsg.ToolCalls = toolCalls
			}
			openaiReq.Messages = append(openaiReq.Messages, openaiMsg)
			continue
		}

		// ── 情形 B: user 消息，可能混合 text / image / tool_result ────────
		var toolResultMsgs []model.Message
		var regularContents []model.MessageContent
		for _, c := range contents {
			switch c.Type {
			case "tool_result":
				// Anthropic tool_result → OpenAI role=tool
				toolResultMsgs = append(toolResultMsgs, model.Message{
					Role:       "tool",
					Content:    c.Content,
					ToolCallId: c.ToolUseId,
				})
			case "text":
				if c.Text != "" {
					regularContents = append(regularContents, model.MessageContent{
						Type: model.ContentTypeText,
						Text: c.Text,
					})
				}
			case "image":
				if c.Source != nil {
					url := fmt.Sprintf("data:%s;base64,%s", c.Source.MediaType, c.Source.Data)
					regularContents = append(regularContents, model.MessageContent{
						Type:     model.ContentTypeImageURL,
						ImageURL: &model.ImageURL{Url: url},
					})
				}
			}
		}
		if len(regularContents) == 1 && regularContents[0].Type == model.ContentTypeText {
			openaiReq.Messages = append(openaiReq.Messages, model.Message{
				Role:    "user",
				Content: regularContents[0].Text,
			})
		} else if len(regularContents) > 0 {
			openaiReq.Messages = append(openaiReq.Messages, model.Message{
				Role:    "user",
				Content: regularContents,
			})
		}
		// tool_result 消息紧跟 assistant 消息之后
		openaiReq.Messages = append(openaiReq.Messages, toolResultMsgs...)
	}

	// tools
	for _, t := range req.Tools {
		openaiReq.Tools = append(openaiReq.Tools, model.Tool{
			Type: "function",
			Function: model.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters: map[string]any{
					"type":       t.InputSchema.Type,
					"properties": t.InputSchema.Properties,
					"required":   t.InputSchema.Required,
				},
			},
		})
	}

	// tool_choice: Anthropic → OpenAI 映射
	if req.ToolChoice != nil {
		if tc, ok := req.ToolChoice.(map[string]any); ok {
			switch tc["type"] {
			case "auto":
				openaiReq.ToolChoice = "auto"
			case "any":
				openaiReq.ToolChoice = "required"
			case "tool":
				if name, ok := tc["name"].(string); ok {
					openaiReq.ToolChoice = map[string]any{
						"type":     "function",
						"function": map[string]any{"name": name},
					}
				}
			case "none":
				openaiReq.ToolChoice = "none"
			}
		}
	}

	return openaiReq
}

func openAIStopReasonToAnthropic(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

// openAIRespToAnthropic 将 OpenAI 非流式响应转换为 Anthropic 格式
func openAIRespToAnthropic(resp *openai.TextResponse, modelName string) *anthropicAdaptor.Response {
	rawId := strings.TrimPrefix(resp.Id, "chatcmpl-")
	if !strings.HasPrefix(rawId, "msg_") {
		rawId = "msg_" + rawId
	}
	ar := &anthropicAdaptor.Response{
		Id:      rawId,
		Type:    "message",
		Role:    "assistant",
		Model:   modelName,
		Content: []anthropicAdaptor.Content{},
		Usage: anthropicAdaptor.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
	for _, choice := range resp.Choices {
		if choice.Message.Content != nil {
			var text string
			if s, ok := choice.Message.Content.(string); ok {
				text = s
			}
			if text != "" {
				ar.Content = append(ar.Content, anthropicAdaptor.Content{Type: "text", Text: text})
			}
		}
		for _, tc := range choice.Message.ToolCalls {
			args := map[string]any{}
			if s, ok := tc.Function.Arguments.(string); ok {
				_ = json.Unmarshal([]byte(s), &args)
			}
			ar.Content = append(ar.Content, anthropicAdaptor.Content{
				Type:  "tool_use",
				Id:    tc.Id,
				Name:  tc.Function.Name,
				Input: args,
			})
		}
		if choice.FinishReason != "" {
			sr := openAIStopReasonToAnthropic(choice.FinishReason)
			ar.StopReason = &sr
		}
	}
	return ar
}

// RelayAnthropicHelper 处理 Anthropic 原生格式（/v1/messages）请求
func RelayAnthropicHelper(c *gin.Context) *model.ErrorWithStatusCode {
	ctx := c.Request.Context()
	m := meta.GetByContext(c)

	// 解析 Anthropic 原生请求体
	anthropicReq := &anthropicAdaptor.Request{}
	if err := common.UnmarshalBodyReusable(c, anthropicReq); err != nil {
		return openai.ErrorWrapper(err, "invalid_anthropic_request", http.StatusBadRequest)
	}
	if anthropicReq.Model == "" {
		return openai.ErrorWrapper(fmt.Errorf("model is required"), "invalid_request", http.StatusBadRequest)
	}

	m.IsStream = anthropicReq.Stream
	m.OriginModelName = anthropicReq.Model
	actualModel, _ := getMappedModelName(anthropicReq.Model, m.ModelMapping)
	m.ActualModelName = actualModel
	anthropicReq.Model = actualModel
	// 强制将转发路径改为 OpenAI chat completions，避免后端节点收到 /v1/messages
	m.RequestURLPath = "/v1/chat/completions"

	// 转换为 OpenAI 通用格式
	openaiReq := anthropicNativeRequestToOpenAI(anthropicReq)
	openaiReq.Model = actualModel

	// 计费预扣
	modelRatio := billingratio.GetModelRatio(actualModel, m.ChannelType)
	groupRatio := billingratio.GetGroupRatio(m.Group)
	ratio := modelRatio * groupRatio
	promptTokens := getPromptTokens(openaiReq, relaymode.ChatCompletions)
	m.PromptTokens = promptTokens
	preConsumedQuota, bizErr := preConsumeQuota(ctx, openaiReq, promptTokens, ratio, m)
	if bizErr != nil {
		return bizErr
	}

	// 获取适配器
	adaptor := relayrelay.GetAdaptor(m.APIType)
	if adaptor == nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid api type: %d", m.APIType), "invalid_api_type", http.StatusBadRequest)
	}
	adaptor.Init(m)

	// 构建请求体（经适配器转换）
	requestBody, err := getRequestBody(c, m, openaiReq, adaptor)
	if err != nil {
		return openai.ErrorWrapper(err, "convert_request_failed", http.StatusInternalServerError)
	}

	// 发送请求
	resp, err := adaptor.DoRequest(c, m, requestBody)
	if err != nil {
		logger.Errorf(ctx, "DoRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if isErrorHappened(m, resp) {
		billing.ReturnPreConsumedQuota(ctx, preConsumedQuota, m.TokenId)
		return RelayErrorHandler(resp)
	}

	// 处理响应并转换格式
	var usage *model.Usage
	var respErr *model.ErrorWithStatusCode
	if anthropicReq.Stream {
		usage, respErr = anthropicStreamRelay(c, resp, m.PromptTokens)
	} else {
		usage, respErr = anthropicNonStreamRelay(c, resp, actualModel)
	}
	if respErr != nil {
		logger.Errorf(ctx, "relay response error: %+v", respErr)
		billing.ReturnPreConsumedQuota(ctx, preConsumedQuota, m.TokenId)
		return respErr
	}
	go postConsumeQuota(ctx, usage, m, openaiReq, ratio, preConsumedQuota, modelRatio, groupRatio, false)
	return nil
}

// anthropicNonStreamRelay 非流式：读取 OpenAI 响应并转换为 Anthropic 格式输出
func anthropicNonStreamRelay(c *gin.Context, resp *http.Response, modelName string) (*model.Usage, *model.ErrorWithStatusCode) {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, openai.ErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	var openaiResp openai.TextResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, openai.ErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
	}
	anthropicResp := openAIRespToAnthropic(&openaiResp, modelName)
	jsonResp, err := json.Marshal(anthropicResp)
	if err != nil {
		return nil, openai.ErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(jsonResp)
	usage := &model.Usage{
		PromptTokens:     openaiResp.Usage.PromptTokens,
		CompletionTokens: openaiResp.Usage.CompletionTokens,
		TotalTokens:      openaiResp.Usage.TotalTokens,
	}
	// 若后端未返回 usage，用本地 token 计数估算（与 openai.Handler 保持一致）
	if usage.TotalTokens == 0 || (usage.PromptTokens == 0 && usage.CompletionTokens == 0) {
		completionTokens := 0
		for _, choice := range openaiResp.Choices {
			completionTokens += openai.CountTokenText(choice.Message.StringContent(), modelName)
		}
		usage.CompletionTokens = completionTokens
		usage.TotalTokens = usage.PromptTokens + completionTokens
	}
	return usage, nil
}

// anthropicStreamRelay 流式：将 OpenAI SSE 转换为 Anthropic SSE 格式输出
// 若后端已返回 Anthropic 格式 SSE，则直接透传
// renderAnthropicEvent 输出带 event: 字段的标准 Anthropic SSE 事件
// Anthropic SDK 依赖 event: 字段来识别事件类型

// anthropicMsgId 生成一个随机的消息 id
func anthropicMsgId() string {
	return fmt.Sprintf("msg_%016x", uint64(helper.GetTimestamp())*0x9e3779b97f4a7c15)
}

func renderAnthropicEvent(c *gin.Context, eventType string, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	// SSE 规范：每个字段单独一次 Write，确保换行符不被代理层合并
	_, _ = c.Writer.WriteString("event: " + eventType + "\n")
	_, _ = c.Writer.WriteString("data: " + string(jsonData) + "\n\n")
	c.Writer.Flush()
}

func anthropicStreamRelay(c *gin.Context, resp *http.Response, promptTokens int) (*model.Usage, *model.ErrorWithStatusCode) {
	common.SetEventStreamHeaders(c)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	// 默认 64KB buffer 不足以处理大上下文（Claude Code 场景 system prompt 可达数百KB）
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := strings.Index(string(data), "\n"); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	var usage model.Usage
	usage.PromptTokens = promptTokens

	// 探测第一个有效 data 行，判断格式
	// Anthropic SSE: type 字段为 message_start / content_block_start 等
	// OpenAI SSE: 含 choices 字段
	isAnthropicFormat := false
	formatDetected := false
	started := false
	blockStopped := false
	passthroughMessageStop := false
	passthroughModel := "" // 透传模式下记录后端返回的 model 名
	index := 0
	var msgId, modelName string

	var completionTokens int

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		// 探测格式
		if !formatDetected {
			formatDetected = true
			var probe map[string]any
			if err := json.Unmarshal([]byte(data), &probe); err == nil {
				_, hasType := probe["type"]
				_, hasChoices := probe["choices"]
				if hasType && !hasChoices {
					isAnthropicFormat = true
				}
			}
		}

		if isAnthropicFormat {
			var event map[string]any
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				render.StringData(c, "data: "+data)
				continue
			}
			eventType, _ := event["type"].(string)
			switch eventType {
			case "message_start":
				if msg, ok := event["message"].(map[string]any); ok {
					// 补全后端可能返回的空 id / model
					if id, ok := msg["id"].(string); !ok || id == "" || id == "msg_" {
						msg["id"] = anthropicMsgId()
					}
					if mdl, ok := msg["model"].(string); !ok || mdl == "" {
						if passthroughModel != "" {
							msg["model"] = passthroughModel
						}
					} else {
						passthroughModel = mdl
					}
					if u, ok := msg["usage"].(map[string]any); ok {
						// 保留后端返回的 cache_creation_input_tokens / cache_read_input_tokens 等字段
						// Claude Code SDK 对 usage 结构有严格校验，缺失 cache 字段会导致解析失败
						u["input_tokens"] = promptTokens
						if _, ok := u["cache_creation_input_tokens"]; !ok {
							u["cache_creation_input_tokens"] = 0
						}
						if _, ok := u["cache_read_input_tokens"]; !ok {
							u["cache_read_input_tokens"] = 0
						}
					}
				}
				renderAnthropicEvent(c, "message_start", event)
				// 若上游未发送 ping，主动补发，Claude Code SDK 依赖此心跳确认连接就绪
				renderAnthropicEvent(c, "ping", map[string]any{"type": "ping"})
				continue
			case "content_block_delta":
				if delta, ok := event["delta"].(map[string]any); ok {
					if delta["type"] == "text_delta" {
						if t, ok := delta["text"].(string); ok {
							completionTokens += len(strings.Fields(t))
						}
					}
				}
			case "message_delta":
				if u, ok := event["usage"].(map[string]any); ok {
					if outTok, ok := u["output_tokens"].(float64); ok && outTok > 0 {
						usage.CompletionTokens = int(outTok)
					} else {
						u["output_tokens"] = completionTokens
						usage.CompletionTokens = completionTokens
					}
					delete(u, "input_tokens")
				}
			case "ping":
				// ping 事件直接透传，无需修改
			case "message_stop":
				// 后端已发送 message_stop，标记不再重复发送
				passthroughMessageStop = true
			}
			renderAnthropicEvent(c, eventType, event)
		} else {
			var chunk openai.ChatCompletionsStreamResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				logger.SysError("unmarshal stream chunk failed: " + err.Error())
				continue
			}
			if !started {
				started = true
				rawId := strings.TrimPrefix(chunk.Id, "chatcmpl-")
				if !strings.HasPrefix(rawId, "msg_") {
					rawId = "msg_" + rawId
				}
				msgId = rawId
				modelName = chunk.Model
				renderAnthropicEvent(c, "message_start", map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id": msgId, "type": "message", "role": "assistant",
						"model": modelName, "content": []any{},
						"stop_reason": nil, "stop_sequence": nil,
						"usage": map[string]any{
							"input_tokens":                promptTokens,
							"output_tokens":               0,
							"cache_creation_input_tokens": 0,
							"cache_read_input_tokens":     0,
						},
					},
				})
				// Claude Code SDK 期望在 message_start 之后收到 ping 心跳
				renderAnthropicEvent(c, "ping", map[string]any{"type": "ping"})
				renderAnthropicEvent(c, "content_block_start", map[string]any{
					"type": "content_block_start", "index": index,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
			}
			for _, choice := range chunk.Choices {
				if text, ok := choice.Delta.Content.(string); ok && text != "" {
					completionTokens++
					renderAnthropicEvent(c, "content_block_delta", map[string]any{
						"type": "content_block_delta", "index": index,
						"delta": map[string]any{"type": "text_delta", "text": text},
					})
				}
				if choice.FinishReason != nil && *choice.FinishReason != "" && *choice.FinishReason != "null" {
					sr := openAIStopReasonToAnthropic(*choice.FinishReason)
					blockStopped = true
					renderAnthropicEvent(c, "content_block_stop", map[string]any{
						"type": "content_block_stop", "index": index,
					})
					renderAnthropicEvent(c, "message_delta", map[string]any{
						"type": "message_delta",
						"delta": map[string]any{"stop_reason": sr, "stop_sequence": nil},
						"usage": map[string]any{"output_tokens": completionTokens},
					})
				}
			}
			if chunk.Usage != nil && chunk.Usage.CompletionTokens > 0 {
				usage.CompletionTokens = chunk.Usage.CompletionTokens
				usage.TotalTokens = chunk.Usage.TotalTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		logger.SysError("error reading stream: " + err.Error())
	}
	// 若流结束时未收到 finish_reason，补发 content_block_stop 和 message_delta
	if started && !blockStopped {
		renderAnthropicEvent(c, "content_block_stop", map[string]any{
			"type": "content_block_stop", "index": index,
		})
		renderAnthropicEvent(c, "message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": completionTokens},
		})
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = completionTokens
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	// 仅在 OpenAI 转换模式下补发 message_stop（透传模式后端已发送过）
	if !passthroughMessageStop {
		renderAnthropicEvent(c, "message_stop", map[string]any{"type": "message_stop"})
	}
	return &usage, nil
}
