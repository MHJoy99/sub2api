-- Backfill historical $0 token rows with the restored fallback price cards.
-- Idempotent: only touches rows where total_cost=0 AND actual_cost=0 AND tokens>0.
WITH cards(model, i, o, cr) AS (VALUES
  ('gemini-3.7-flash', 0.75e-6, 3.75e-6, 0.075e-6),
  ('gemini-3.7-flash-tiered', 0.75e-6, 3.75e-6, 0.075e-6),
  ('alibaba-token-plan-qwen3.8-max', 2e-6, 6e-6, 0.25e-6),
  ('go-qwen3.8-max', 2e-6, 6e-6, 0.25e-6),
  ('alibaba-token-plan-qwen3.7-plus', 0.4e-6, 1.2e-6, 0.05e-6),
  ('alibaba-token-plan-qwen3.7-max', 0.4e-6, 1.2e-6, 0.05e-6),
  ('go-qwen3.7-plus', 0.4e-6, 1.2e-6, 0.05e-6),
  ('alibaba-token-plan-qwen3.6-flash', 0.1e-6, 0.4e-6, 0.01e-6),
  ('gemini-pro-agent', 2e-6, 12e-6, 0.2e-6),
  ('gemini-3-pro-high', 2e-6, 12e-6, 0.2e-6),
  ('gemini-3-pro-low', 2e-6, 12e-6, 0.2e-6),
  ('gemini-3-pro-preview', 2e-6, 12e-6, 0.2e-6),
  ('tab_flash_lite_preview', 0.1e-6, 0.4e-6, 0.01e-6),
  ('gemini-2.5-flash-thinking', 0.3e-6, 2.5e-6, 0.03e-6),
  ('joyvoice-fast-audio', 0.3e-6, 2.5e-6, 0.03e-6),
  ('go-muse-spark-1.2', 1.25e-6, 4.25e-6, 0.15e-6),
  ('go-muse-spark-1.2-contributor', 0.1e-6, 0.2e-6, 0.002e-6),
  ('go-muse-spark-1.3-contributor', 0.1e-6, 0.2e-6, 0.002e-6),
  ('go-mimo-v2.5', 0.1e-6, 0.3e-6, 0.02e-6),
  ('gpt-oss-120b-medium', 0.15e-6, 0.6e-6, 0.03e-6)
)
UPDATE usage_logs u SET
  input_cost        = u.input_tokens * c.i,
  output_cost       = u.output_tokens * c.o,
  cache_read_cost   = u.cache_read_tokens * c.cr,
  cache_creation_cost = 0,
  total_cost        = u.input_tokens * c.i + u.output_tokens * c.o + u.cache_read_tokens * c.cr,
  actual_cost       = (u.input_tokens * c.i + u.output_tokens * c.o + u.cache_read_tokens * c.cr)
                      * COALESCE(u.rate_multiplier, 1)
FROM cards c
WHERE u.model = c.model
  AND u.total_cost = 0
  AND u.actual_cost = 0
  AND (u.input_tokens + u.output_tokens + u.cache_read_tokens) > 0
  AND COALESCE(u.billing_mode, 'token') = 'token';
