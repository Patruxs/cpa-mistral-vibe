package main

import (
	"sync"
	"time"
)

const mistralBaseURL = "https://api.mistral.ai/v1"

type modelCaps struct {
	CompletionChat bool `json:"completion_chat"`
	Reasoning      bool `json:"reasoning"`
	Vision         bool `json:"vision"`
}

type mistralModel struct {
	ID           string    `json:"id"`
	Capabilities modelCaps `json:"capabilities"`
}

type modelsListResp struct {
	Data []mistralModel `json:"data"`
}

var (
	modelsCacheMu sync.Mutex
	modelsCache   []mistralModel
	modelsCacheAt time.Time
)

const modelsCacheTTL = 10 * time.Minute

// discoverModels returns the chat-capable models from Mistral's /v1/models,
// cached briefly. Native discovery, no hard-coded catalog. Falls back to the
// two known models if the key or network is unavailable.
func discoverModels() []mistralModel {
	modelsCacheMu.Lock()
	if len(modelsCache) > 0 && time.Since(modelsCacheAt) < modelsCacheTTL {
		cached := modelsCache
		modelsCacheMu.Unlock()
		return cached
	}
	modelsCacheMu.Unlock()

	key, err := resolveKey()
	if err != nil {
		return fallbackModels()
	}
	var out modelsListResp
	if err := mistralGet(key, "/v1/models", &out); err != nil {
		return fallbackModels()
	}
	chat := make([]mistralModel, 0, len(out.Data))
	for _, m := range out.Data {
		if m.Capabilities.CompletionChat {
			chat = append(chat, m)
		}
	}
	if len(chat) == 0 {
		return fallbackModels()
	}
	modelsCacheMu.Lock()
	modelsCache = chat
	modelsCacheAt = time.Now()
	modelsCacheMu.Unlock()
	return chat
}

func fallbackModels() []mistralModel {
	return []mistralModel{
		{ID: "deepseek-v4-1-flash-1017", Capabilities: modelCaps{CompletionChat: true, Reasoning: true}},
		{ID: "zai-glm-5-3-flash", Capabilities: modelCaps{CompletionChat: true, Reasoning: true}},
	}
}

func modelsJSON() []byte {
	src := discoverModels()
	models := make([]map[string]any, 0, len(src))
	for _, m := range src {
		entry := map[string]any{
			"ID":          m.ID, // native id, sent upstream as-is
			"Name":        m.ID,
			"Object":      "model",
			"OwnedBy":     "mistral",
			"DisplayName": m.ID,
			"Type":        "chat",
		}
		if m.Capabilities.Reasoning {
			entry["Thinking"] = map[string]any{
				"Min":            0,
				"Max":            128000,
				"ZeroAllowed":    true,
				"DynamicAllowed": true,
				"Levels":         []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
			}
		}
		models = append(models, entry)
	}
	return mustOK(map[string]any{"Provider": providerKey, "Models": models})
}
