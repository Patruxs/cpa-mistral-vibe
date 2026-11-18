package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	consoleBase = "https://console.mistral.ai"
	consoleAPI  = consoleBase + "/api"
)

// loginState is the opaque State carried between StartLogin and PollLogin.
type loginState struct {
	Verifier  string `json:"v"`
	ProcessID string `json:"p"`
	PollURL   string `json:"u"`
}

func (s loginState) encode() string {
	raw, _ := json.Marshal(s)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLoginState(s string) (loginState, error) {
	var out loginState
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

type signInStart struct {
	SignInURL string `json:"sign_in_url"`
	PollURL   string `json:"poll_url"`
	ProcessID string `json:"process_id"`
	ExpiresAt string `json:"expires_at"`
}

// vibeStart begins the browser PKCE sign-in and returns the user-facing URL and
// the opaque poll state. Mirrors heyhuynhgiabuu/pi-oauth-mistral and get-key.mjs.
func vibeStart() (urlStr string, state loginState, expires time.Time, err error) {
	verifierBytes := make([]byte, 64)
	if _, err = rand.Read(verifierBytes); err != nil {
		return
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	body, _ := json.Marshal(map[string]string{
		"code_challenge":        challenge,
		"code_challenge_method": "S256",
	})
	var start signInStart
	if err = consolePost(consoleAPI+"/vibe/sign-in", body, &start); err != nil {
		return
	}
	if err = sameOrigin(start.SignInURL, consoleBase); err != nil {
		return
	}
	if err = sameOrigin(start.PollURL, consoleAPI); err != nil {
		return
	}
	expires, _ = time.Parse(time.RFC3339, start.ExpiresAt)
	state = loginState{Verifier: verifier, ProcessID: start.ProcessID, PollURL: start.PollURL}
	return start.SignInURL, state, expires, nil
}

type pollResult struct {
	Status        string `json:"status"`
	ExchangeToken string `json:"exchange_token"`
	Message       string `json:"message"`
}

type exchangeResult struct {
	APIKey string `json:"api_key"`
}

// vibePoll checks the sign-in once. Returns (status, apiKey, error) where status
// is "pending", "completed", or "expired".
func vibePoll(state loginState) (status, apiKey string, err error) {
	resp, body, httpErr := consoleGet(state.PollURL)
	if httpErr != nil {
		return "", "", httpErr
	}
	if resp == http.StatusGone {
		return "expired", "", nil
	}
	var r pollResult
	if e := json.Unmarshal(body, &r); e != nil {
		return "", "", fmt.Errorf("poll decode: %w", e)
	}
	switch r.Status {
	case "completed":
		key, exErr := vibeExchange(state, r.ExchangeToken)
		if exErr != nil {
			return "", "", exErr
		}
		return "completed", key, nil
	case "pending", "":
		return "pending", "", nil
	default:
		msg := r.Status
		if r.Message != "" {
			msg += ": " + r.Message
		}
		return "", "", fmt.Errorf("sign-in %s", msg)
	}
}

func vibeExchange(state loginState, exchangeToken string) (string, error) {
	if exchangeToken == "" {
		return "", fmt.Errorf("sign-in completed without an exchange token")
	}
	payload, _ := json.Marshal(map[string]string{
		"exchange_token": exchangeToken,
		"code_verifier":  state.Verifier,
	})
	var ex exchangeResult
	exchangeURL := fmt.Sprintf("%s/vibe/sign-in/%s/exchange", consoleAPI, url.PathEscape(state.ProcessID))
	if err := consolePost(exchangeURL, payload, &ex); err != nil {
		return "", err
	}
	if ex.APIKey == "" {
		return "", fmt.Errorf("exchange returned no api_key")
	}
	return ex.APIKey, nil
}

// ---- small console HTTP helpers ----

func consolePost(urlStr string, body []byte, out any) error {
	req, err := http.NewRequest(http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mistral-usage-plugin/"+pluginVersion)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
	}
	return nil
}

func consoleGet(urlStr string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mistral-usage-plugin/"+pluginVersion)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, nil
}

func sameOrigin(raw, base string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad url %q: %w", raw, err)
	}
	b, err := url.Parse(base)
	if err != nil {
		return err
	}
	if u.Scheme != b.Scheme || u.Host != b.Host {
		return fmt.Errorf("unexpected url origin: %s", raw)
	}
	return nil
}
