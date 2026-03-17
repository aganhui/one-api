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
	"github.com/songquanpeng/one-api/common/logger"
	"github.com/songquanpeng/one-api/common/render"
	relayrelay "github.com/songquanpeng/one-api/relay"
	anthropic "github.com/songquanpeng/one-api/relay/adaptor/anthropic"
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
func anthropicNativeRequestToOpenAI(req *anthropic.Request) *model.GeneralOpenAIRequest {
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
		var contents []model.MessageContent
		for _, c := range msg.ParseContents() {
			switch c.Type {
			case "text":
				contents = append(contents, model.MessageContent{
					Type: model.ContentTypeText,
					Text: c.Text,
				})
			case "image":
				if c.Source != nil {
					url := fmt.Sprintf("data:%s;base64,%s", c.Source.MediaType, c.Source.Data)
					contents = append(contents, model.MessageContent{
						Type:     model.ContentTypeImageURL,
						ImageURL: &model.ImageURL{Url: url},
					})
				}
			}
		}
		if len(contents) == 1 && contents[0].Type == model.ContentTypeText {
			openaiReq.Messages = append(openaiReq.Messages, model.Message{
				Role:    msg.Role,
				Content: contents[0].Text,
			})
		} else if len(contents) > 0 {
			openaiReq.Messages = append(openaiReq.Messages, model.Message{
				Role:    msg.Role,
				Content: contents,
			})
		}
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
func openAIRespToAnthropic(resp *openai.TextResponse, modelName string) *anthropic.Response {
	ar := &anthropic.Response{
		Id:    strings.TrimPrefix(resp.Id, "chatcmpl-"),
		Type:  "message",
		Role:  "assistant",
		Model: modelName,
		Usage: anthropic.Usage{
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
				ar.Content = append(ar.Content, anthropic.Content{Type: "text", Text: text})
			}
		}
		for _, tc := range choice.Message.ToolCalls {
			args := map[string]any{}
			if s, ok := tc.Function.Arguments.(string); ok {
				_ = json.Unmarshal([]byte(s), &args)
			}
			ar.Content = append(ar.Content, anthropic.Content{
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
	anthropicReq := &anthropic.Request{}
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
func anthropicStreamRelay(c *gin.Context, resp *http.Response, promptTokens int) (*model.Usage, *model.ErrorWithStatusCode) {
	common.SetEventStreamHeaders(c)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
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
	started := false
	index := 0
	var msgId, modelName string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk openai.ChatCompletionsStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			logger.SysError("unmarshal stream chunk failed: " + err.Error())
			continue
		}

		if !started {
			started = true
			msgId = strings.TrimPrefix(chunk.Id, "chatcmpl-")
			modelName = chunk.Model
			// message_start
			_ = render.ObjectData(c, map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id": msgId, "type": "message", "role": "assistant",
					"model": modelName, "content": []any{},
					"stop_reason": nil, "stop_sequence": nil,
					"usage": map[string]any{"input_tokens": promptTokens, "output_tokens": 0},
				},
			})
			// content_block_start
			_ = render.ObjectData(c, map[string]any{
				"type": "content_block_start", "index": index,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
		}

		for _, choice := range chunk.Choices {
			if text, ok := choice.Delta.Content.(string); ok && text != "" {
				_ = render.ObjectData(c, map[string]any{
					"type": "content_block_delta", "index": index,
					"delta": map[string]any{"type": "text_delta", "text": text},
				})
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" && *choice.FinishReason != "null" {
				sr := openAIStopReasonToAnthropic(*choice.FinishReason)
				// content_block_stop
				_ = render.ObjectData(c, map[string]any{
					"type": "content_block_stop", "index": index,
				})
				// message_delta
				_ = render.ObjectData(c, map[string]any{
					"type": "message_delta",
					"delta": map[string]any{
						"stop_reason": sr, "stop_sequence": nil,
					},
					"usage": map[string]any{"input_tokens": promptTokens, "output_tokens": usage.CompletionTokens},
				})
			}
		}
		// 累计 usage（若 chunk 包含 usage 字段）
		if chunk.Usage != nil {
			usage.PromptTokens = chunk.Usage.PromptTokens
			usage.CompletionTokens = chunk.Usage.CompletionTokens
			usage.TotalTokens = chunk.Usage.TotalTokens
		}
	}

	if err := scanner.Err(); err != nil {
		logger.SysError("error reading stream: " + err.Error())
	}
	// 若后端未返回 usage，用提示词 token 数补全
	if usage.TotalTokens == 0 || (usage.PromptTokens == 0 && usage.CompletionTokens == 0) {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	// message_stop
	_ = render.ObjectData(c, map[string]any{"type": "message_stop"})
	render.Done(c)
	return &usage, nil
}
