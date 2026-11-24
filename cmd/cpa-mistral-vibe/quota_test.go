package main

import "testing"

func TestGroupLabelsFitQuotaCard(t *testing.T) {
	g := group("Vibe Code", budgetPool{UsagePercentage: 0.98, InitialBudget: 150, Currency: "USD", ResetAt: "2026-12-01T00:00:00Z"})
	if got := g["displayName"]; got != "Vibe Code · resets Dec 1" {
		t.Fatalf("group displayName = %q", got)
	}
	if got := g["description"]; got != "Resets 2026-12-01 00:00 UTC" {
		t.Fatalf("group description = %q", got)
	}
	b := g["buckets"].([]map[string]any)[0]
	if got := b["displayName"]; got != "$1.47 / $150.00" {
		t.Fatalf("bucket displayName = %q", got)
	}
	if got := b["description"]; got != "$1.47 of $150.00 used" {
		t.Fatalf("bucket description = %q", got)
	}
	if _, ok := b["resetTime"]; ok {
		t.Fatal("bucket must not carry resetTime: the host renders it as a long date that squeezes out the label")
	}
	if got := b["remainingFraction"].(float64); got < 0.99 || got > 0.991 {
		t.Fatalf("remainingFraction = %v", got)
	}
}

func TestGroupWithoutParsableReset(t *testing.T) {
	g := group("API", budgetPool{UsagePercentage: 250, InitialBudget: 10})
	if got := g["displayName"]; got != "API" {
		t.Fatalf("group displayName = %q", got)
	}
	if _, ok := g["description"]; ok {
		t.Fatal("unexpected group description")
	}
	b := g["buckets"].([]map[string]any)[0]
	if got := b["remainingFraction"].(float64); got != 0 {
		t.Fatalf("remainingFraction = %v", got)
	}
}
