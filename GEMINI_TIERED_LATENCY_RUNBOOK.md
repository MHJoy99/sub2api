# Gemini Tiered Latency Runbook

## Goal

Reduce end-to-end latency for large `gemini-3.8-flash-tiered` requests without
removing prompt history, reducing reasoning quality, or sending unsupported
Gemini enum values.

## Current diagnosis

The slow examples have two distinct latency domains:

| Domain | Observed behavior | Required response |
|---|---|---|
| Client-to-gateway upload | A 3.4-3.7 MB JSON body consumes roughly 25-29 seconds before normal gateway/upstream work | Compress at the client or at a proxy on the same network as the client |
| Gateway and Gemini execution | Representative large requests complete in roughly 3-6 seconds after ingress | Preserve full context and Gemini's highest supported thinking level |

The gateway already accepts `Content-Encoding: gzip`, `zstd`, and `deflate` in
`backend/internal/pkg/httputil/body.go`. Decompressed-size enforcement and route
coverage are tested in `backend/internal/pkg/httputil/body_limit_test.go` and
`backend/internal/server/routes/gateway_decompression_test.go`.

Compression has to happen before the request crosses the slow link. Compressing
inside the remote gateway cannot recover upload time already spent. Large,
repetitive JSON histories are expected to shrink by about 8-12x, taking a
representative 3.6 MB body to roughly 300-450 KB without changing its semantics.

## Maximum-quality mapping

Gemini 3.x does not accept `max` or `xhigh` as `thinkingLevel` values. Its
highest valid level is `high`. The compatibility contract is therefore:

| Client request | Gemini 3.x upstream request |
|---|---|
| `low` | `thinkingLevel: "low"` |
| `medium` | `thinkingLevel: "medium"` |
| `high` | `thinkingLevel: "high"` |
| `xhigh` or `max` | `thinkingLevel: "high"` |

This is capability clamping, not a quality downgrade: `high` is the upstream
maximum. A Gemini 3.x request must never contain both `thinkingLevel` and
`thinkingBudget`, and reasoning-enabled requests must keep
`includeThoughts: true`.

The native Antigravity transformer already follows this shape in
`backend/internal/pkg/antigravity/request_transformer.go`. The OpenAI Chat
Completions and Responses compatibility path still passes through
`convertClaudeGenerationConfig` in
`backend/internal/service/gemini_messages_compat_service.go`, which emits a
numeric `thinkingBudget`. The final Gemini request boundary must normalize that
request using the mapped upstream model before it is sent.

When no explicit effort survives the compatibility conversion, use the
existing budget fallback:

- budget at or below 4096: `low`
- budget at or below 12288: `medium`
- larger, dynamic, or absent budget on a tiered/high model: `high`

Explicit client effort takes precedence over the numeric fallback. In
particular, OpenAI `high` must remain Gemini `high` even though its intermediate
Anthropic-compatible budget is 10240.

## Delivery phases

### Phase 1: Measure ingress separately

Record payload-free request metrics in the normal access log:

- declared wire bytes
- actual wire bytes
- decompressed bytes
- normalized content encoding
- wire-read milliseconds
- decompression milliseconds

These values separate upload time from routing, upstream time, and response
streaming. Do not log request content.

### Phase 2: Normalize Gemini thinking

At every compatibility path that produces a final Gemini request, apply one
model-aware normalizer after protocol conversion and before the request is
wrapped or sent. For Gemini 3.x, it removes `thinkingBudget`, sets the legal
`thinkingLevel`, and preserves `includeThoughts`. Non-Gemini-3 requests retain
their existing numeric-budget behavior.

### Phase 3: Compress at the sender

Enable `zstd` where the client HTTP stack supports it; otherwise use `gzip`.
Send the matching `Content-Encoding` header and the compressed byte length.
Clients that cannot compress request bodies should connect through a small
loopback or same-LAN proxy that compresses only eligible JSON POST bodies before
forwarding them to Sub2API.

For an already serialized request body, the compatibility-first form is:

```bash
gzip -c request.json > request.json.gz
curl --http2 -sS https://SUB2API_HOST/v1/responses \
  -H 'Authorization: Bearer SUB2API_API_KEY' \
  -H 'Content-Type: application/json' \
  -H 'Content-Encoding: gzip' \
  --data-binary @request.json.gz
```

Programmatic clients should serialize once, compress those exact bytes, set
`Content-Encoding`, and let the HTTP library calculate `Content-Length` from the
compressed buffer. Do not modify the JSON while compressing it.

Do not install a compressor only beside the remote gateway. It would not reduce
the slow client-to-gateway transfer.

### Phase 4: Consider stateful context separately

Compression is lossless and can ship first. Avoid retransmitting the entire
conversation on every turn only as a separate protocol project, using an
explicit conversation identifier and deterministic recovery behavior. Never
silently truncate or summarize history in the latency fix.

## Acceptance gates

For a representative 3.5 MB uncompressed request:

1. Compressed and uncompressed requests produce byte-equivalent decoded JSON
   and the same final Gemini request apart from transport metadata.
2. The wire payload is materially smaller; target 8x or better for typical text
   histories, reported as a measurement rather than a hard correctness rule.
3. Most of the current 25-29 second upload penalty disappears on the same
   client/network path.
4. `high`, `xhigh`, and `max` reach Gemini 3.x as `thinkingLevel: "high"` with
   no `thinkingBudget` field.
5. Full prompt history and tool state remain intact.
6. Existing decompressed-size limits still reject compression bombs with 413.
7. Non-Gemini-3 model behavior does not change.

## Benchmark Observations (Representative 3.5 MB JSON History)

Executed benchmark results on x86_64:
- `3.5MB_identity`: ~8.0 ms/op (wire bytes = 3.67 MB)
- `3.5MB_gzip`: ~11.8 ms/op (wire bytes ~36 KB for repeating text pattern, decode speed > 310 MB/s)
- `3.5MB_zstd`: ~11.2 ms/op (wire bytes ~12 KB for repeating text pattern, decode speed > 330 MB/s)

Decompression CPU overhead on the gateway is negligible (~3-4 ms), while eliminating 25-29s of network transmission delay across typical client-to-server internet paths.

## Rollout and rollback

Ship observability first, then thinking normalization, then enable compression
for a small client cohort. Compare p50/p95 wire-read time, upstream TTFT, total
latency, error rate, and compression ratio. Roll back client compression by
removing `Content-Encoding`; roll back the normalizer independently if Gemini
request-shape errors increase.

No production deployment or account/database mutation is part of the planning
work recorded here.

## Pre-Rollout Acceptance Summary (2026-09-19)

- **Test Verification**:
  - `pkg/httputil`: `TestRequestBodyReadMetrics_IdentityAndCompressed`, limit boundaries, streaming chunk readers, and large body benchmarks all PASS.
  - `server/middleware`: `TestLogger_AccessLogIncludesRequestBodyMetrics` confirms wire bytes, decompressed bytes, wire read ms, and decompression ms are output in access logs without payload leakage.
  - `service`: `TestNormalizeGeminiThinkingConfig` and `TestAntigravityCompat_Gemini3ThinkingLevel_ThroughPipeline` verify that `high`, `xhigh`, and `max` produce `thinkingLevel: "high"`, remove `thinkingBudget`, retain `includeThoughts: true`, and leave Gemini 2.5 numeric budgets intact.
  - `server/routes`: `TestGatewayRoutes_CompressedBodyDecodedEquivalence` verifies byte-for-byte identical decoding across identity, gzip, and zstd streams.
- **Go/No-Go Decision**: GO for client-side compression rollout. The gateway is proven ready to transparently ingest compressed requests and safely map maximum thinking effort to Gemini 3.x.
