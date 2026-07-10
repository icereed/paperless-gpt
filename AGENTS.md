# AGENTS.md

paperless-gpt is a Go backend with a React/TypeScript frontend (in `web-app/`) that provides AI-powered document processing for paperless-ngx: OCR enhancement, title/tag generation, and metadata extraction via LLMs.

## Project structure

```
paperless-gpt/
├── main.go                    # Go backend entry point (web server on port 8080)
├── ocr/                       # OCR provider implementations
├── default_prompts/           # Default AI prompt templates
├── web-app/                   # React/TypeScript frontend
│   ├── src/                   # React source code
│   ├── dist/                  # Built frontend (created by npm run build)
│   └── e2e/                   # Playwright E2E tests
├── docs/                      # Documentation
├── .github/workflows/         # CI/CD pipelines
└── Dockerfile                 # Multi-stage Docker build
```

## Setup and build

System dependency: mupdf (`apt-get install -y mupdf libmupdf-dev` on Debian/Ubuntu, `brew install mupdf` on macOS).

Build the frontend first — the Go binary embeds it from `dist/` at the repo root:

```bash
cd web-app && npm install && npm run build && cp -r dist ../ && cd ..
go mod download
go build -o paperless-gpt
```

Full pipeline takes ~2 minutes. Builds and tests are slow but reliable — never cancel a long-running build; use generous timeouts (npm: 300s+, go build/test: 600s+, docker build: 1800s+).

## Testing

```bash
go test ./...                  # Backend tests, ~1 min
cd web-app && npm run lint     # Frontend linting (ESLint)
gofmt -l .                     # Go formatting check (must print nothing)
```

- `npm test` in `web-app/` is currently a placeholder (`echo "TODO"`) — frontend unit tests don't exist yet.
- Some Go tests need network access (tiktoken encoding downloads); failures from that are expected in restricted environments and are not code issues.

### E2E tests (Playwright + TestContainers, requires Docker)

```bash
cd web-app
npm run test:e2e:mock          # Secret-free mock-LLM run (no API keys needed) — use this by default
npm run test:e2e               # Full suite; real-LLM specs need API keys
```

## Running the application

Minimal configuration:

```bash
export PAPERLESS_BASE_URL="http://localhost:8000"
export PAPERLESS_API_TOKEN="your_token_here"
export LLM_PROVIDER="ollama"
export LLM_MODEL="test_model"
export OLLAMA_HOST="http://localhost:11434"
./paperless-gpt                # Web UI at http://localhost:8080
```

The app starts gracefully without a reachable paperless-ngx instance. On first run it creates `prompts/` from `default_prompts/`. Any OpenAI-compatible endpoint works via `OPENAI_BASE_URL`.

To validate a change end-to-end: start the app, verify the web server comes up on port 8080, check the API routes in the startup logs, and confirm the frontend is served from `dist/`.

## Common pitfalls

- **Frontend changes not visible**: re-run `npm run build` and `cp -r dist ../` — the Go binary serves the copied `dist/`, not `web-app/dist/`.
- **mupdf build errors**: install the `mupdf`/`libmupdf-dev` system packages.
- **Docker build fails**: needs external network access (Alpine repositories); `docker build -t paperless-gpt .` takes 5+ minutes.

## CI and PR guidelines

- `.github/workflows/docker-build-and-push.yml` — secret-free PR pipeline: Go/frontend tests, multi-arch Docker builds (AMD64 + ARM64), mock-LLM E2E. Registry pushes and secrets only run outside `pull_request`.
- `.github/workflows/e2e-real-llm.yml` — real-LLM E2E behind an environment-approval gate (maintainer-triggered).
- Before committing: run `cd web-app && npm run lint` and check `gofmt -l .` reports nothing.
- Commit messages follow Conventional Commits (`feat:`, `fix:`, `ci:`, `docs:` — see `git log`).
