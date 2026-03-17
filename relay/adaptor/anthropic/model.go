package anthropic

import "strings"

// https://docs.anthropic.com/claude/reference/messages_post

type Metadata struct {
	UserId string `json:"user_id"`
}

type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type Content struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *ImageSource `json:"source,omitempty"`
	// tool_calls
	Id        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	Content   string `json:"content,omitempty"`
	ToolUseId string `json:"tool_use_id,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string 或 []Content
}

// StringContent 将 content 统一转为字符串
func (m Message) StringContent() string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if mp, ok := item.(map[string]any); ok {
				if mp["type"] == "text" {
					if t, ok := mp["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

// ParseContents 将 content 解析为 []Content
func (m Message) ParseContents() []Content {
	switch v := m.Content.(type) {
	case string:
		return []Content{{Type: "text", Text: v}}
	case []any:
		var contents []Content
		for _, item := range v {
			mp, ok := item.(map[string]any)
			if !ok {
				continue
			}
			c := Content{}
			if t, ok := mp["type"].(string); ok {
				c.Type = t
			}
			if t, ok := mp["text"].(string); ok {
				c.Text = t
			}
			if id, ok := mp["id"].(string); ok {
				c.Id = id
			}
			if name, ok := mp["name"].(string); ok {
				c.Name = name
			}
			if toolUseId, ok := mp["tool_use_id"].(string); ok {
				c.ToolUseId = toolUseId
			}
			if content, ok := mp["content"].(string); ok {
				c.Content = content
			}
			if src, ok := mp["source"].(map[string]any); ok {
				c.Source = &ImageSource{}
				if t, ok := src["type"].(string); ok {
					c.Source.Type = t
				}
				if mt, ok := src["media_type"].(string); ok {
					c.Source.MediaType = mt
				}
				if d, ok := src["data"].(string); ok {
					c.Source.Data = d
				}
			}
			contents = append(contents, c)
		}
		return contents
	}
	return nil
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"input_schema"`
}

type InputSchema struct {
	Type       string `json:"type"`
	Properties any    `json:"properties,omitempty"`
	Required   any    `json:"required,omitempty"`
}

type Request struct {
	Model         string    `json:"model"`
	Messages      []Message `json:"messages"`
	System        any       `json:"system,omitempty"` // string 或 []Content
	MaxTokens     int       `json:"max_tokens,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
	Stream        bool      `json:"stream,omitempty"`
	Temperature   *float64  `json:"temperature,omitempty"`
	TopP          *float64  `json:"top_p,omitempty"`
	TopK          int       `json:"top_k,omitempty"`
	Tools         []Tool    `json:"tools,omitempty"`
	ToolChoice    any       `json:"tool_choice,omitempty"`
	//Metadata    `json:"metadata,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Error struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type Response struct {
	Id           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Content      []Content `json:"content"`
	Model        string    `json:"model"`
	StopReason   *string   `json:"stop_reason"`
	StopSequence *string   `json:"stop_sequence"`
	Usage        Usage     `json:"usage"`
	Error        Error     `json:"error"`
}

type Delta struct {
	Type         string  `json:"type"`
	Text         string  `json:"text"`
	PartialJson  string  `json:"partial_json,omitempty"`
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type StreamResponse struct {
	Type         string    `json:"type"`
	Message      *Response `json:"message"`
	Index        int       `json:"index"`
	ContentBlock *Content  `json:"content_block"`
	Delta        *Delta    `json:"delta"`
	Usage        *Usage    `json:"usage"`
}
