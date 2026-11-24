package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func handleQuotaFetch(request []byte) []byte {
	var req struct {
		StorageJSON []byte `json:"storage_json"`
		Provider    string `json:"provider"`
	}
	_ = json.Unmarshal(request, &req)

	key := ""
	if m := keyPattern.Find(req.StorageJSON); m != nil {
		key = string(m)
	}
	if key == "" {
		// Fall back to a key in the plugin config / CPA config file.
		if k, err := resolveKey(); err == nil {
			key = k
		}
	}
	if key == "" {
		return errorEnvelope("no_key", "no Mistral key in credential", 0)
	}

	budget, err := fetchBudget(key)
	if err != nil {
		return errorEnvelope("fetch_failed", err.Error(), 0)
	}
	plan := ""
	if sub := fetchSubscription(key); sub != nil {
		plan = sub.PlanName
	}

	resp := map[string]any{
		"subscription": map[string]any{"plan": plan},
		"summary": []map[string]any{
			metric("vibe_used", "Vibe Code used", budget.VibeBudget.used()),
			metric("vibe_total", "Vibe Code allowance", budget.VibeBudget.InitialBudget),
			metric("api_used", "API used", budget.APIBudget.used()),
		},
		"groups": []map[string]any{
			group("Vibe Code", budget.VibeBudget),
			group("API", budget.APIBudget),
		},
	}
	return mustOK(resp)
}

func metric(key, label string, value float64) map[string]any {
	return map[string]any{
		"key":      key,
		"label":    label,
		"value":    round2(value),
		"format":   "currency",
		"currency": "USD",
	}
}

func group(name string, p budgetPool) map[string]any {
	remaining := 1 - p.UsagePercentage/100
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 1 {
		remaining = 1
	}
	cur := p.Currency
	if cur == "" {
		cur = "USD"
	}
	used, total := money(p.used(), cur), money(p.InitialBudget, cur)
	g := map[string]any{
		"displayName": name,
		"buckets": []map[string]any{{
			"bucketId":          strings.ToLower(strings.ReplaceAll(name, " ", "-")),
			"displayName":       used + " / " + total,
			"remainingFraction": remaining,
			"description":       fmt.Sprintf("%s of %s used", used, total),
		}},
	}
	if reset, err := time.Parse(time.RFC3339, p.ResetAt); err == nil {
		reset = reset.UTC()
		g["displayName"] = name + " · resets " + reset.Format("Jan 2")
		g["description"] = "Resets " + reset.Format("2006-01-02 15:04") + " UTC"
	}
	return g
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
