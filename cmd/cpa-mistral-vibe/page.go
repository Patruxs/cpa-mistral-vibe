package main

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"
)

func renderPage() string {
	key, err := resolveKey()
	if err != nil {
		return pageShell(errorCard("Mistral key not found", err.Error()))
	}
	budget, err := fetchBudget(key)
	if err != nil {
		return pageShell(errorCard("Could not read Mistral billing", err.Error()))
	}
	sub := fetchSubscription(key)

	plan := "Mistral"
	if sub != nil && sub.PlanName != "" {
		plan = "Mistral " + sub.PlanName
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<div class="head"><div><h1>Mistral Usage</h1><div class="plan">%s</div></div>`, html.EscapeString(plan))
	fmt.Fprintf(&b, `<div class="updated">Updated %s · auto-refresh 60s</div></div>`, time.Now().Format("15:04:05 MST"))
	b.WriteString(`<div class="cards">`)
	b.WriteString(allowanceCard("Vibe Code Allowance", budget.VibeBudget))
	b.WriteString(allowanceCard("API Allowance", budget.APIBudget))
	b.WriteString(`</div>`)
	return pageShell(b.String())
}

func allowanceCard(title string, p budgetPool) string {
	pct := p.UsagePercentage
	if pct < 0 {
		pct = 0
	}
	barPct := math.Min(pct, 100)
	color := "#3fb950" // green
	switch {
	case pct >= 90:
		color = "#f85149" // red
	case pct >= 70:
		color = "#d29922" // amber
	}
	cur := p.Currency
	if cur == "" {
		cur = "USD"
	}
	reset := formatReset(p.ResetAt)
	payg := "off"
	if p.PaygEnabled {
		payg = "on"
	}
	return fmt.Sprintf(`<div class="card">
  <div class="card-top"><span class="title">%s</span><span class="pct">%.1f%% used</span></div>
  <div class="amount">%s <span class="muted">of %s</span></div>
  <div class="bar"><div class="fill" style="width:%.2f%%;background:%s"></div></div>
  <div class="meta"><span>%s remaining</span><span>%s</span></div>
  <div class="meta"><span>pay-as-you-go %s</span></div>
</div>`,
		html.EscapeString(title),
		pct,
		money(p.used(), cur), money(p.InitialBudget, cur),
		barPct, color,
		money(p.remaining(), cur), reset,
		payg,
	)
}

func money(v float64, cur string) string {
	sym := "$"
	if cur != "USD" && cur != "" {
		sym = ""
		return fmt.Sprintf("%.2f %s", v, cur)
	}
	return fmt.Sprintf("%s%.2f", sym, v)
}

func formatReset(raw string) string {
	if raw == "" {
		return "reset date unknown"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "resets " + raw
	}
	days := int(math.Ceil(time.Until(t).Hours() / 24))
	if days < 0 {
		days = 0
	}
	return fmt.Sprintf("resets %s (in %d days)", t.Format("2006-01-02"), days)
}

func errorCard(title, detail string) string {
	return fmt.Sprintf(`<div class="head"><h1>Mistral Usage</h1></div>
<div class="card err"><div class="title">%s</div><div class="errdetail">%s</div></div>`,
		html.EscapeString(title), html.EscapeString(detail))
}

func pageShell(body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="60">
<title>Mistral Usage</title>
<style>
:root{color-scheme:dark light}
*{box-sizing:border-box}
body{margin:0;padding:24px;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
  background:#0d1117;color:#e6edf3}
.head{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin-bottom:20px;flex-wrap:wrap}
h1{font-size:20px;margin:0}
.plan{color:#8b949e;font-size:13px;margin-top:2px}
.updated{color:#6e7681;font-size:12px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:16px}
.card{background:#161b22;border:1px solid #30363d;border-radius:10px;padding:16px}
.card-top{display:flex;justify-content:space-between;align-items:baseline;margin-bottom:8px}
.title{font-weight:600}
.pct{color:#8b949e;font-size:13px}
.amount{font-size:22px;font-weight:650;margin-bottom:10px}
.muted{color:#8b949e;font-size:14px;font-weight:400}
.bar{height:8px;background:#21262d;border-radius:999px;overflow:hidden}
.fill{height:100%;border-radius:999px;transition:width .3s}
.meta{display:flex;justify-content:space-between;color:#8b949e;font-size:12px;margin-top:8px}
.err{border-color:#f85149}
.err .title{color:#f85149}
.errdetail{color:#8b949e;font-size:13px;margin-top:8px;word-break:break-word;font-family:ui-monospace,monospace}
</style></head><body>` + body + `</body></html>`
}
