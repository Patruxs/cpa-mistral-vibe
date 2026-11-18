package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// storageDoc is the on-disk / round-tripped credential payload for a Mistral auth.
type storageDoc struct {
	Type      string `json:"type"`
	APIKey    string `json:"api_key"`
	Label     string `json:"label,omitempty"`
	Plan      string `json:"plan,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

func credentialID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "mistral-vibe-" + hex.EncodeToString(sum[:])[:12]
}

func buildAuthData(key, fileName string) map[string]any {
	id := credentialID(key)
	label := "Mistral Vibe Code"
	plan := ""
	if sub := fetchSubscription(key); sub != nil && sub.PlanName != "" {
		plan = sub.PlanName
		label = "Mistral " + sub.PlanName + " (Vibe)"
	}
	if fileName == "" {
		fileName = id + ".json"
	}
	storage, _ := json.Marshal(storageDoc{
		Type:      providerKey,
		APIKey:    key,
		Label:     label,
		Plan:      plan,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return map[string]any{
		"Provider":    providerKey,
		"ID":          id,
		"FileName":    fileName,
		"Label":       label,
		"StorageJSON": storage, // []byte -> base64 in JSON; host decodes to StorageJSON
		"Metadata":    map[string]any{"type": providerKey, "plan": plan},
		// base_url + api_key let the host's built-in OpenAI-compat executor run
		// inference against Mistral with this credential's key.
		"Attributes": map[string]string{
			"type":     providerKey,
			"base_url": mistralBaseURL,
			"api_key":  key,
		},
	}
}

// ---- auth.parse: recognize mistral-*.json credential files on load ----

func handleAuthParse(request []byte) []byte {
	var req struct {
		Provider string `json:"Provider"`
		Path     string `json:"Path"`
		FileName string `json:"FileName"`
		RawJSON  []byte `json:"RawJSON"`
	}
	_ = json.Unmarshal(request, &req)

	isOurs := false
	var doc storageDoc
	if err := json.Unmarshal(req.RawJSON, &doc); err == nil && doc.Type == providerKey {
		isOurs = true
	}
	key := ""
	if m := keyPattern.Find(req.RawJSON); m != nil {
		key = string(m)
	}
	if !isOurs {
		// Accept any file named mistral-*.json that carries a key.
		if key != "" && len(req.FileName) >= 8 && req.FileName[:8] == "mistral-" {
			isOurs = true
		}
	}
	if !isOurs || key == "" {
		return mustOK(map[string]any{"Handled": false})
	}
	auth := buildAuthData(key, req.FileName)
	// Preserve the original file bytes as storage so re-parsing stays stable.
	auth["StorageJSON"] = req.RawJSON
	return mustOK(map[string]any{"Handled": true, "Auth": auth})
}

// ---- auth.login.start / poll: Vibe browser OAuth ----

func handleLoginStart(request []byte) []byte {
	urlStr, state, expires, err := vibeStart()
	if err != nil {
		return errorEnvelope("login_start_failed", err.Error(), 0)
	}
	if expires.IsZero() {
		expires = time.Now().Add(10 * time.Minute)
	}
	token := putSession(state, expires)
	return mustOK(map[string]any{
		"Provider":  providerKey,
		"URL":       urlStr,
		"State":     token,
		"ExpiresAt": expires.UTC().Format(time.RFC3339),
		"Metadata":  map[string]any{},
	})
}

func handleLoginPoll(request []byte) []byte {
	var req struct {
		State string `json:"State"`
	}
	_ = json.Unmarshal(request, &req)
	state, ok := getSession(req.State)
	if !ok {
		return mustOK(map[string]any{"Status": "error", "Message": "sign-in expired, start again"})
	}
	status, key, err := vibePoll(state)
	if err != nil {
		return mustOK(map[string]any{"Status": "error", "Message": err.Error()})
	}
	switch status {
	case "pending":
		return mustOK(map[string]any{"Status": "pending", "Message": "waiting for Mistral sign-in"})
	case "expired":
		dropSession(req.State)
		return mustOK(map[string]any{"Status": "error", "Message": "sign-in expired, start again"})
	case "completed":
		dropSession(req.State)
		return mustOK(map[string]any{
			"Status":  "success",
			"Message": "signed in",
			"Auth":    buildAuthData(key, ""),
		})
	default:
		return mustOK(map[string]any{"Status": "error", "Message": "unexpected sign-in status"})
	}
}

// ---- auth.refresh: Vibe keys are long-lived; validate and keep ----

func handleAuthRefresh(request []byte) []byte {
	var req struct {
		AuthID      string            `json:"AuthID"`
		StorageJSON []byte            `json:"StorageJSON"`
		Attributes  map[string]string `json:"Attributes"`
	}
	_ = json.Unmarshal(request, &req)
	auth := map[string]any{
		"Provider":    providerKey,
		"ID":          req.AuthID,
		"StorageJSON": req.StorageJSON,
		"Attributes":  map[string]string{"type": providerKey},
	}
	return mustOK(map[string]any{
		"Auth":             auth,
		"NextRefreshAfter": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
}
