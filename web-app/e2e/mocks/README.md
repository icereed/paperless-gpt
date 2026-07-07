# Mock LLM stubs (WireMock)

These WireMock stub mappings emulate the OpenAI `/v1/chat/completions` API so
the main E2E flow can run without real LLM API keys — on every PR, including
forks, at zero cost.

Each mapping matches a distinctive phrase from the corresponding prompt
template in `default_prompts/` and returns a fixed, deterministic completion.
If you change a phrase in a prompt template, update the matching stub here.

Usage: `npm run test:e2e:mock` (sets `E2E_LLM_MODE=mock`, which makes
`test-environment.ts` start a WireMock container and point paperless-gpt at it
via `OPENAI_BASE_URL`).

These stubs verify the full document-processing pipeline (UI, backend,
paperless-ngx integration) — not LLM output quality. Real-LLM E2E tests run
separately after maintainer approval.
