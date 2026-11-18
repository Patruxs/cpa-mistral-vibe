package main

// Response normalization ported from cpa-mistral-normalizer (MIT, (c) 2026
// batuhan4, https://github.com/Batuhan4/cpa-mistral-normalizer) so this plugin
// is self-contained: users install only cpa-mistral-vibe.
//
// Mistral's chat-completions API returns assistant content as a list of chunks
// when the model reasons:
//
//	"content": [
//	  {"type": "thinking", "thinking": [{"type": "text", "text": "..."}], "closed": true},
//	  {"type": "text", "text": "391"}
//	]
//
// OpenAI-style clients and CLIProxyAPI's translators expect content to be a
// string, with reasoning as a separate reasoning_content string. This flattens
// the former into the latter (streaming and non-streaming) and leaves
// everything else alone.

import (
	"bytes"
	"encoding/json"
	"strings"
)

// transformRequest mirrors the host's ResponseTransformRequest (no JSON tags on
// the host side, so field names are the Go names; []byte is base64).
type transformRequest struct {
	FromFormat        string
	ToFormat          string
	Model             string
	Stream            bool
	OriginalRequest   json.RawMessage
	TranslatedRequest json.RawMessage
	Body              []byte
}

// payloadResponse mirrors the host's PayloadResponse. An empty Body tells the
// host to keep the payload it already has, byte for byte.
type payloadResponse struct {
	Body []byte `json:",omitempty"`
}

// handleNormalizeBefore is the response.normalize_before RPC entry point.
func handleNormalizeBefore(request []byte) []byte {
	req, err := decodeTransformRequest(request)
	if err != nil {
		return errorEnvelope("bad_request", "decode transform request", http_StatusBadRequest)
	}
	return mustOK(payloadResponse{Body: normalizeResponse(req)})
}

const http_StatusBadRequest = 400

// normalizeResponse returns the rewritten upstream payload, or nil to keep it.
// Scoped cheaply: only OpenAI chat-completions responses whose content is a
// list get touched, so non-Mistral traffic is a no-op.
func normalizeResponse(req transformRequest) []byte {
	if req.FromFormat != "openai" || !mayNeedChange(req.Body, true) {
		return nil
	}
	out, changed := normalizePayload(req.Body, true)
	if !changed {
		return nil
	}
	return out
}

var (
	markOriginal   = []byte(`,"OriginalRequest":`)
	markTranslated = []byte(`,"TranslatedRequest":`)
	markBody       = []byte(`,"Body":`)
)

// decodeTransformRequest decodes the host's request without scanning the two
// request bodies attached to every call (including every stream chunk).
func decodeTransformRequest(request []byte) (transformRequest, error) {
	var req transformRequest
	i := bytes.Index(request, markOriginal)
	j := bytes.LastIndex(request, markBody)
	if i > 0 && j > i && bytes.HasSuffix(request, []byte("}")) {
		header := make([]byte, 0, i+1)
		header = append(header, request[:i]...)
		header = append(header, '}')
		var head struct {
			FromFormat string
			ToFormat   string
			Model      string
			Stream     bool
		}
		var body []byte
		if json.Unmarshal(header, &head) == nil && json.Unmarshal(request[j+len(markBody):len(request)-1], &body) == nil {
			req.FromFormat, req.ToFormat, req.Model, req.Stream = head.FromFormat, head.ToFormat, head.Model, head.Stream
			req.Body = body
			if k := bytes.Index(request[i:j], markTranslated); k >= 0 {
				req.TranslatedRequest = json.RawMessage(request[i+k+len(markTranslated) : j])
			}
			return req, nil
		}
	}
	err := json.Unmarshal(request, &req)
	return req, err
}

// mayNeedChange is a cheap byte scan that rules out payloads this would never
// touch, before any JSON decoding.
func mayNeedChange(body []byte, stripResponseError bool) bool {
	if stripResponseError && bytes.Contains(body, []byte(`"response_error"`)) {
		return true
	}
	key := []byte(`"content"`)
	rest := body
	for {
		i := bytes.Index(rest, key)
		if i < 0 {
			return false
		}
		rest = rest[i+len(key):]
		j := skipSpace(rest, 0)
		if j < len(rest) && rest[j] == ':' {
			j = skipSpace(rest, j+1)
			if j < len(rest) && rest[j] == '[' {
				return true
			}
		}
	}
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// normalizePayload rewrites one upstream payload: a whole non-streaming
// response, or one stream line with or without its "data: " prefix.
func normalizePayload(body []byte, stripResponseError bool) ([]byte, bool) {
	prefix, doc := splitSSE(body)
	if len(doc) == 0 || doc[0] != '{' {
		return body, false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(doc, &top) != nil {
		return body, false
	}
	rawChoices, ok := top["choices"]
	if !ok {
		return body, false
	}
	var choices []json.RawMessage
	if json.Unmarshal(rawChoices, &choices) != nil {
		return body, false
	}
	changed := false
	for i, rawChoice := range choices {
		var choice map[string]json.RawMessage
		if json.Unmarshal(rawChoice, &choice) != nil || choice == nil {
			continue
		}
		choiceChanged := false
		for _, key := range []string{"message", "delta"} {
			rawMsg, ok := choice[key]
			if !ok {
				continue
			}
			var msg map[string]json.RawMessage
			if json.Unmarshal(rawMsg, &msg) != nil || msg == nil {
				continue
			}
			if normalizeMessage(msg, key == "delta") {
				enc, err := marshalNoEscape(msg)
				if err != nil {
					return body, false
				}
				choice[key] = enc
				choiceChanged = true
			}
		}
		if stripResponseError {
			if _, ok := choice["response_error"]; ok {
				delete(choice, "response_error")
				choiceChanged = true
			}
		}
		if choiceChanged {
			enc, err := marshalNoEscape(choice)
			if err != nil {
				return body, false
			}
			choices[i] = enc
			changed = true
		}
	}
	if !changed {
		return body, false
	}
	encChoices, err := marshalNoEscape(choices)
	if err != nil {
		return body, false
	}
	top["choices"] = encChoices
	encTop, err := marshalNoEscape(top)
	if err != nil {
		return body, false
	}
	out := make([]byte, 0, len(prefix)+len(encTop))
	out = append(out, prefix...)
	out = append(out, encTop...)
	return out, true
}

// splitSSE separates a "data:" prefix from the JSON document that follows.
func splitSSE(body []byte) (prefix, doc []byte) {
	trimmed := bytes.TrimSpace(body)
	if rest, ok := bytes.CutPrefix(trimmed, []byte("data:")); ok {
		return []byte("data: "), bytes.TrimSpace(rest)
	}
	return nil, trimmed
}

// normalizeMessage rewrites a message or delta in place.
func normalizeMessage(msg map[string]json.RawMessage, delta bool) bool {
	rawContent, ok := msg["content"]
	if !ok {
		return false
	}
	trimmed := bytes.TrimSpace(rawContent)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return false
	}
	var chunks []map[string]json.RawMessage
	if json.Unmarshal(trimmed, &chunks) != nil {
		return false
	}
	var text, thinking strings.Builder
	for _, chunk := range chunks {
		var typ string
		if json.Unmarshal(chunk["type"], &typ) != nil {
			return false
		}
		switch typ {
		case "text":
			var s string
			if json.Unmarshal(chunk["text"], &s) != nil {
				return false
			}
			text.WriteString(s)
		case "thinking":
			s, ok := thinkingText(chunk["thinking"])
			if !ok {
				return false
			}
			thinking.WriteString(s)
		default:
			return false
		}
	}

	if thinking.Len() > 0 {
		reasoning := ""
		if prev, ok := msg["reasoning_content"]; ok {
			_ = json.Unmarshal(prev, &reasoning)
		}
		enc, err := marshalNoEscape(reasoning + thinking.String())
		if err != nil {
			return false
		}
		msg["reasoning_content"] = enc
	}

	switch {
	case text.Len() > 0:
		enc, err := marshalNoEscape(text.String())
		if err != nil {
			return false
		}
		msg["content"] = enc
	case delta:
		delete(msg, "content")
	case hasToolCalls(msg):
		msg["content"] = json.RawMessage("null")
	default:
		msg["content"] = json.RawMessage(`""`)
	}
	return true
}

// thinkingText flattens a thinking chunk's payload (a list of text chunks, or
// defensively a plain string).
func thinkingText(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", true
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(trimmed, &s) != nil {
			return "", false
		}
		return s, true
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(trimmed, &parts) != nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range parts {
		raw, ok := part["text"]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		b.WriteString(s)
	}
	return b.String(), true
}

func hasToolCalls(msg map[string]json.RawMessage) bool {
	raw, ok := msg["tool_calls"]
	if !ok {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("[]"))
}

// marshalNoEscape encodes v without HTML escaping and without a trailing newline.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}
