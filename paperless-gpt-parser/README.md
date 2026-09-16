# paperless-gpt-parser

> **Status:** Reference implementation skeleton. Tracks the design discussion
> in [icereed/paperless-gpt#964](https://github.com/icereed/paperless-gpt/pull/964).
> Will move to its own repository once the API stabilises.

A thin Python adapter that registers as a [paperless-ngx 3.0 parser plugin]
(https://github.com/paperless-ngx/paperless-ngx/pull/12294) and forwards
documents to a running [paperless-gpt](https://github.com/icereed/paperless-gpt)
sidecar over HTTP.

```
┌──────────────────────────┐         ┌──────────────────────────┐
│ paperless-ngx Celery     │ entry-  │ paperless-gpt-parser     │
│ worker (Python)          │ point   │ (this package)           │
│                          │ ──────▶ │ implements ParserProtocol│
└──────────────────────────┘         └────────────┬─────────────┘
                                                  │ HTTP
                                                  ▼
                                  ┌──────────────────────────────┐
                                  │ paperless-gpt sidecar (Go)   │
                                  │ POST /api/v1/parse           │
                                  └──────────────────────────────┘
```

## Why a shim and not a native Python plugin?

paperless-gpt is written in Go. Bundling a Go shared library in a Python
wheel across architectures and Python versions is a packaging nightmare; an
HTTP call to a sidecar is universal, trivially auditable, and matches how
most paperless-gpt users already deploy.

## Install (planned)

```bash
pip install paperless-gpt-parser
```

In your paperless-ngx Docker image / venv. The entrypoint group
`paperless_ngx.parsers` is auto-discovered by paperless-ngx 3.0 on worker
startup.

## Configure

| Env var | Default | Purpose |
|---|---|---|
| `PAPERLESS_GPT_URL` | `http://paperless-gpt:8080` | Base URL of the sidecar |
| `PAPERLESS_GPT_API_TOKEN` | *(unset)* | Optional bearer token if the sidecar requires auth |
| `PAPERLESS_GPT_PARSER_ENABLED` | `true` | Master kill-switch |
| `PAPERLESS_GPT_PARSER_SCORE` | *(from /capabilities, fallback 50)* | Score returned when MIME type is supported. Built-in Tesseract returns ~10. |
| `PAPERLESS_GPT_PARSER_TIMEOUT` | `300` | Per-document HTTP timeout (seconds) |

## Test it without paperless-ngx

```bash
PAPERLESS_GPT_URL=http://localhost:8080 \
  python -c "from paperless_gpt_parser.client import GptClient; \
             import pathlib; \
             print(GptClient().parse(pathlib.Path('test.pdf'), 'application/pdf').text[:200])"
```

## Status / what works today

This package targets the **MVP** API (text only). Archive PDF + thumbnail
pass-through will land once the sidecar implements them.

| ParserProtocol method | Status |
|---|---|
| `name` / `version` / `author` / `url` | ✅ |
| `supported_mime_types()` | ✅ (from `/capabilities`) |
| `score()` | ✅ |
| `parse()` + `get_text()` | ✅ |
| `get_page_count()` | ✅ |
| `get_thumbnail()` | 🚧 placeholder (renders a 1×1 WebP) |
| `get_archive_path()` | 🚧 returns `None` (no archive yet) |
| `get_date()` | 🚧 returns `None` |
| `extract_metadata()` | 🚧 returns `[]` |
| `__enter__` / `__exit__` | ✅ |

## Layout

```
paperless-gpt-parser/
├── pyproject.toml
├── README.md             ← this file
└── src/paperless_gpt_parser/
    ├── __init__.py
    ├── parser.py         ← GptParser implementing ParserProtocol
    ├── client.py         ← httpx client for paperless-gpt
    └── config.py         ← env-var parsing
```
