# cpa-mistral-vibe

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that adds
**Mistral** as a provider through the **Vibe browser sign-in** (`console.mistral.ai`),
so requests draw on your subscription's **Vibe Code allowance** instead of a
pay-as-you-go API key.

It does three things:

- **OAuth login** — a browser PKCE sign-in (the same flow the Mistral Vibe CLI
  uses) stored as a CPA credential, so you don't paste a static key. Multiple
  Mistral accounts can be added.
- **Quota card** — the monthly **Vibe Code** and **API** allowances
  (used / total, % remaining, reset date) in the native *Quota Management* page,
  read live from Mistral's billing endpoint.
- **Native model discovery** — the chat-capable models are listed dynamically
  from Mistral's `/v1/models` (DeepSeek, GLM/Z.ai, Mistral, Magistral, Ministral,
  Codestral, Voxtral, …). No hard-coded catalog.

Inference itself runs on CPA's built-in OpenAI-compatible executor: the plugin
stores `base_url` and `api_key` on the credential's attributes, and the host
handles translation, streaming and request logging.

## Install

From the CLIProxyAPI Management Center, install from this repository's release
(`install.type: github-release`), then enable it:

```yaml
plugins:
  enabled: true
  configs:
    cpa-mistral-vibe:
      enabled: true
```

Restart or reload CPA, open **Quota Management → Plugin**, and use the Mistral
**OAuth login** to sign in. A `mistral-<id>.json` credential is created and its
Vibe Code / API allowance appears as a card.

### Build manually

Requires Go 1.26 with cgo (the plugin is a `c-shared` library).

```bash
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -trimpath -buildvcs=false -buildmode=c-shared -ldflags="-s -w" \
  -o cpa-mistral-vibe.so ./cmd/cpa-mistral-vibe
```

Place `cpa-mistral-vibe.so` under `<plugins.dir>/linux/<arch>/` and add the
config block above. Tagged releases (`vX.Y.Z`) build `linux/amd64` and
`linux/arm64` automatically.

## Configuration

| Field | Description |
|---|---|
| `api-key` | Optional. A `mstrl_…` key to use directly. Leave empty to use the signed-in credential (recommended). |
| `config-path` | Optional. Path to the CPA config file the plugin may read a key from. Default `/app/config.yaml`. |

The provider key is `mistral`; credentials are `mistral-*.json` auth files.

## Request normalization

Mistral has a few quirks that OpenAI-shaped clients (e.g. Codex) trip on. These
are handled by stock CPA mechanisms keyed on the model name, not by this plugin,
so configure them once for the Mistral model families you use:

- **`reasoning_effort: medium` is rejected** by most models — map it to `high`
  with a `requests.payload.override` rule.
- **The `developer` role is rejected** — rewrite it to `system`.
- **`reasoning_content` in history is rejected (422)** — drop it with a
  `requests.payload.filter` rule.
- **Reasoning responses return `content` as a chunk list** — flatten it to a
  string + `reasoning_content` with the
  [`cpa-mistral-normalizer`](https://github.com/Batuhan4/cpa-mistral-normalizer)
  plugin (its default `models: ["*"]` is safe; it only rewrites list-shaped
  content).

## Caveats

- Quota reads use Mistral's **internal** `/v1/billing/budget` endpoint, which is
  not a published API and can change without notice. The `$150` / `$15` figures
  are plan constants; the API returns only the used percentage.
- The allowance resets on the **1st of each month, 00:00 UTC**. The Quota card
  renders that in your browser's locale and timezone.
- A fresh sign-in creates a credential keyed by the key's hash (no stable
  account id is available), so re-logging in the same account adds a new entry —
  delete the old one if it duplicates.
- Sharing a Mistral subscription through Vibe for programmatic use may be subject
  to Mistral's terms; use your own account.

## License

MIT
