# Composite Groups

Composite groups are an admin routing layer for API keys that should choose a
concrete provider from the requested model instead of binding the key to a
single provider group. They support both built-in model detection and an
admin-configured model route registry for public model aliases.

## Supported Providers

Composite groups can route to these concrete account platforms:

- Anthropic
- Gemini
- OpenAI
- Antigravity
- Grok
- Kimi
- Zhipu
- DeepSeek

The selected concrete platform is used for account selection, user platform
quota checks, post-usage billing, ops error platform attribution, channel
mapping/pricing lookup, and platform usage reporting.

## Route Registry

Admins can configure routes on a composite group from the group list's
`Routes` action or through the admin API:

- `GET /api/v1/admin/groups/:id/composite-routes`
- `POST /api/v1/admin/groups/:id/composite-routes`
- `PUT /api/v1/admin/groups/:id/composite-routes/:route_id`
- `DELETE /api/v1/admin/groups/:id/composite-routes/:route_id`
- `POST /api/v1/admin/groups/:id/composite-routes/preview`

Each route belongs to one composite group and contains:

- `public_model`: model identifier the client sends.
- `match_type`: `exact` or `prefix`.
- `target_platform`: concrete provider platform.
- `upstream_model`: model identifier sent upstream. If omitted, the public
  model is reused.
- `endpoint`: `any`, `messages`, `count_tokens`, `responses`,
  `chat_completions`, `embeddings`, `images`, or `gemini`.
- `priority`: lower values win after match specificity.
- `enabled`: disabled routes are ignored by runtime resolution but remain
  visible to admins.

Resolution order is explicit route first, then built-in detection. When more
than one explicit route matches, exact matches beat prefix matches,
endpoint-specific routes beat `any`, longer prefixes beat shorter prefixes,
then lower `priority`, then lower route id.

For JSON-body endpoints, the gateway rewrites the request `model` field to the
route's `upstream_model` before dispatch. For Gemini native paths such as
`/v1beta/models/{model}:generateContent`, the gateway resolves `{model}` and
the handler forwards the resolved upstream model.

Codex Alpha Search and Live requests use the `responses` route domain. Live
requests resolve the model from `session.model`, including multipart `session`
payloads, and apply the configured `upstream_model` before dispatch.
Codex model manifest requests reuse the existing OpenAI account selection and
failover path within the Composite group.

## Built-In Detection

Composite routing detects common public model IDs and provider-prefixed IDs:

- `claude-*` and `anthropic/claude-*` route to Anthropic.
- `gemini-*` and `google/gemini-*` route to Gemini.
- `gpt-*`, `o*`, `codex-*`, `text-embedding-*`, `dall-e-*`, and
  `openai/*` route to OpenAI.
- `grok-*` and `xai/grok-*` route to Grok.
- `kimi-*`, `moonshot/*`, and `k3` route to Kimi.
- `glm-*` and `zhipu/*` route to Zhipu.
- `deepseek-*` and `deepseek/*` route to DeepSeek.

Unknown or ambiguous model names fail closed with a client error instead of
guessing a provider.

## Admin Workflows

- Admins can create a group with platform `composite`.
- Admins can add, edit, delete, and preview composite model routes.
- Composite groups can copy accounts from concrete provider groups.
- Concrete provider accounts can be assigned directly to composite groups from
  account create/edit and bulk account workflows.
- Subscription payment plans can bind to a composite group when that group's
  `subscription_type` is `subscription`. The plan grants access to the
  composite group; each request is still billed and quota-checked against the
  resolved concrete provider platform.
- Channel configuration exposes composite groups in concrete provider sections.
  The channel `group_ids` payload is still flat; provider-specific model
  mapping and pricing remain keyed by concrete platform.

## Example Setup: OpenAI + Claude + Gemini + Grok

Use one composite subscription group when one customer-facing plan should expose
model aliases across OpenAI, Claude, Gemini, and Grok without issuing separate
keys per provider. This is an illustrative setup; the deployed instance is
documented in [Production Setup: All Providers](#production-setup-all-providers).

1. Create concrete provider groups for the upstream account pools, for example
   `OpenAI Paid`, `Claude Paid`, `Gemini Paid`, and `Grok Paid`.
2. Create a `composite` group with `subscription_type` set to `subscription`.
3. Assign provider accounts directly to the composite group, or copy accounts
   from the concrete provider groups during group creation.
4. Add explicit routes for public aliases that should not rely on built-in
   model detection:

   | Public model | Endpoint | Target platform | Upstream model |
   | --- | --- | --- | --- |
   | `all/gpt-5` | `responses` | `openai` | `gpt-5` |
   | `all/claude-sonnet` | `messages` | `anthropic` | `claude-sonnet-4-6` |
   | `all/gemini-pro` | `gemini` | `gemini` | `gemini-2.5-pro` |
   | `all/grok` | `responses` | `grok` | `grok-4.3` |

5. Configure channel pricing and model mapping under the concrete platforms
   named in each route. Composite routing does not create pricing records.
6. Create a subscription payment plan for the composite group.

The same composite group can also rely on built-in detection for standard model
names such as `gpt-*`, `claude-*`, `gemini-*`, and `grok-*`. Explicit routes are
recommended for bundled plan aliases because they make endpoint, provider, and
upstream model attribution reviewable in the admin UI.

## Production Setup: All Providers

As of 2026-08-28, the production instance uses separate provider groups plus a
full-access composite group:

| Group | ID | Purpose | Accounts | Routes |
| --- | ---: | --- | ---: | ---: |
| `All Models` | 4 | Existing unified client access | 6 | 8 |
| `Alibaba Token Plan` | 5 | Isolated Alibaba Token Plan account | 1 | 0 |
| `OpenCode Go` | 6 | Isolated OpenCode Go account | 1 | 0 |
| `All Providers` | 7 | One-key access across all configured providers | 8 | 52 |

Group 7 copies accounts from groups 4, 5, and 6 and uses exact routes for the
44 Alibaba and Go aliases. The route target is `openai`, but the route leaves
the upstream model at the public alias so account 23 or 24 can apply its own
account-level mapping. Do not replace these exact routes with one broad
`target_platform=openai` route: both OpenAI-compatible accounts expose some of
the same bare upstream model names, and broad routing can select the wrong
provider.

The owner full-access key is stored locally as `all_providers` in the
mode-600, git-ignored `deploy/owner-api-keys.json`. The existing `all_models`
key remains bound to group 4, while `alibaba_token_plan` and `opencode_go`
remain available for provider-isolated clients. API keys bind to one group;
use group 7 when a client needs one key for all configured models.

Deployed key-to-group map:

| Local key name | Group | Use |
| --- | ---: | --- |
| `all_models` | 4 | Existing unified models only |
| `alibaba_token_plan` | 5 | Alibaba-only access |
| `opencode_go` | 6 | OpenCode Go-only access |
| `all_providers` | 7 | All configured provider models |

The group-4 key intentionally returns HTTP 400 for the isolated Alibaba and Go
aliases. Use `all_providers` for one-key access to those aliases:

```bash
curl https://YOUR_HOST/v1/chat/completions \
  -H "Authorization: Bearer $ALL_PROVIDERS_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"alibaba-token-plan-qwen3.7-plus","messages":[{"role":"user","content":"ready"}]}'
```

Verification for the deployed setup:

- `alibaba-token-plan-qwen3.7-plus` selects account 23 through group 7.
- `go-glm-5` selects account 24 through group 7.
- `gemini-3.7-flash-tiered` selects the antigravity pool through group 7.
- `GET /v1/models` through the group-7 key returns 85 models.

The detailed migration record, database checks, backup path, and API payloads
are maintained in [`PRICING_MAPPING_RUNBOOK.md`](../PRICING_MAPPING_RUNBOOK.md).

## Limits

Composite routes choose a concrete provider and upstream model; they do not
create synthetic model metadata, pricing, or upstream capability records by
themselves. Keep channel pricing/model mapping configured for the concrete
provider platforms that the routes target.

Current limitations:

- AUTO smart-routing among multiple providers for the same abstract task.
- Direct API-key binding to several existing groups without a composite group.
- Protocol-agnostic provider decoupling or a LiteLLM-style adapter rewrite.
