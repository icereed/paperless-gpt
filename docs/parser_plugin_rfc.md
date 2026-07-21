# RFC: A Generic LLM-OCR HTTP API (with paperless-ngx as the first consumer)

> **Status:** Draft / Discussion — **MVP shipped in this PR**
> **Author:** @icereed (and contributors)
> **Headline use case:** paperless-ngx 3.0 [parser plugin framework](https://github.com/paperless-ngx/paperless-ngx/pull/12294) ([discussion #12023](https://github.com/paperless-ngx/paperless-ngx/discussions/12023))
> **Other targeted consumers:** n8n / Zapier / Make, local coding agents (Claude Code, Continue, aider), CLI tools, custom RAG pipelines, anything that struggles with PDFs.
> **Try it:** see [`examples/parser-plugin/README.md`](examples/parser-plugin/README.md) for `curl` quickstart.

## TL;DR

We propose a small, stable, consumer-agnostic HTTP API on `paperless-gpt`:

```
POST /api/v1/parse          # send a document, get text + searchable PDF + thumbnail
GET  /api/v1/capabilities   # what MIME types / providers are available
GET  /api/v1/healthz        # liveness
```

The API has **no paperless-ngx-specific concepts**. It's a generic "LLM-powered OCR/parse" service. The paperless-ngx parser plugin is just a thin Python shim (~200 LOC) on top of it — but the same endpoint is equally useful to:

- **n8n / Zapier / Make** workflows that need to extract text from incoming PDFs.
- **Local coding agents** (Claude Code, Continue, aider, custom MCP servers) that struggle with PDFs in user prompts.
- **CLI tools** like `cat foo.pdf | pgpt parse > foo.txt` for one-off conversions.
- **RAG pipelines** that need a searchable PDF + plain text from arbitrary uploads.
- **Any service** that today wraps Tesseract or a vendor OCR and would prefer LLM quality.

paperless-gpt is uniquely positioned for this: we already have the broadest provider matrix (Mistral OCR, Azure DocAI, Google DocAI, generic Vision LLMs via langchaingo, Docling, Anthropic, Ollama) and produce hOCR + searchable PDFs via [`gardar/ocrchestra`](https://github.com/gardar/ocrchestra). All of that already exists; we just need to expose it through a stable, document-in / structured-data-out endpoint.

This RFC tracks the design discussion. Implementation will land incrementally.

---

## Why this matters

**For the broader ecosystem:**

- "Give me a PDF, get back high-quality text + a searchable PDF" is a problem **everyone** has. Today, every workflow tool (n8n, Make), every coding agent, every RAG framework either ships its own bad Tesseract integration or punts. A free, self-hostable, LLM-grade alternative with a 3-line HTTP call would be widely adopted.
- Coding agents in particular **routinely fail** on PDFs in user prompts (they get raw bytes or terrible text extraction). A simple `curl`-able endpoint they can call solves this for the entire local-AI scene.
- paperless-gpt already has the engine. It's only missing the surface.

**For the paperless-ngx integration specifically:**

- paperless-ngx maintainers ([explicit statement](https://github.com/paperless-ngx/paperless-ngx/discussions/12023#discussioncomment-15737324)) want **OCR/parsing complexity out of core** and into plugins.
- The new framework is the **canonical extension point** going forward — the existing tag/polling integration paperless-gpt uses will keep working, but the plugin path becomes the recommended deployment for new users.
- The provider matrix maps **directly** onto the parser protocol's `get_text` / `get_archive_path` / `get_thumbnail` methods.
- Being the first quality plugin in the slot establishes us as the de-facto standard for LLM-based document parsing in the paperless-ngx ecosystem.

**The strategic point:** by designing the API generically up-front, we get the paperless-ngx integration *and* a much larger TAM at no extra design cost. The paperless-ngx shim becomes a 200-LOC reference consumer that proves the API.

## What the parser protocol expects

From [`src/paperless/parsers/__init__.py`](https://github.com/paperless-ngx/paperless-ngx/blob/dev/src/paperless/parsers/__init__.py) (dev branch, will ship in 3.0):

```python
@runtime_checkable
class ParserProtocol(Protocol):
    name: str
    version: str
    author: str
    url: str

    @classmethod
    def supported_mime_types(cls) -> dict[str, str]: ...
    @classmethod
    def score(cls, mime_type, filename, path) -> int | None: ...

    @property
    def can_produce_archive(self) -> bool: ...
    @property
    def requires_pdf_rendition(self) -> bool: ...

    def configure(self, context: ParserContext) -> None: ...
    def parse(self, document_path, mime_type, *, produce_archive=True) -> None: ...

    def get_text(self) -> str | None: ...
    def get_date(self) -> datetime.datetime | None: ...
    def get_archive_path(self) -> Path | None: ...
    def get_thumbnail(self, document_path, mime_type) -> Path: ...
    def get_page_count(self, document_path, mime_type) -> int | None: ...
    def extract_metadata(self, document_path, mime_type) -> list[MetadataEntry]: ...

    def __enter__(self) -> Self: ...
    def __exit__(self, exc_type, exc_val, exc_tb) -> None: ...
```

Discovery happens via:

```toml
[project.entry-points."paperless_ngx.parsers"]
paperless_gpt = "paperless_gpt_parser.parser:GptParser"
```

External parsers win ties against built-ins. If our `score()` returns `None`, paperless-ngx falls back to the built-in (e.g. Tesseract).

## Proposed architecture

One stateless HTTP service. Many consumers.

```
  ┌──────────────────┐   ┌───────────────────┐   ┌─────────────────┐   ┌──────────────┐   ┌───────────┐
  │ paperless-ngx    │   │ n8n / Zapier /    │   │ coding agents   │   │ RAG / vector │   │ CLI users │
  │ (Python shim)    │   │ Make workflows    │   │ (CC, Continue,  │   │ ingestion    │   │ pgpt CLI  │
  │ ParserProtocol   │   │ HTTP node         │   │ aider, MCP)     │   │ (langchain…) │   │           │
  └────────┬─────────┘   └─────────┬─────────┘   └────────┬────────┘   └──────┬───────┘   └─────┬─────┘
           │                       │                      │                   │                 │
           │                       │   POST /api/v1/parse                     │                 │
           └───────────────────────┴──────────────────────┴───────────────────┴─────────────────┘
                                                          │
                                                          ▼
                                          ┌────────────────────────────┐
                                          │  paperless-gpt (this repo) │
                                          │                            │
                                          │  • Provider matrix         │
                                          │    (Mistral, Azure DocAI,  │
                                          │    Google DocAI, Ollama,   │
                                          │    OpenAI, Anthropic, …)   │
                                          │  • hOCR generation         │
                                          │  • Searchable PDF builder  │
                                          │                            │
                                          │  Runs as a single container│
                                          └────────────────────────────┘
```

### Why HTTP and not a Go shared library / IPC?

- HTTP is **the** universal integration substrate. Every workflow tool, agent, language and runtime can call it. A Go SDK could be added later as a convenience wrapper but should never be a *requirement*.
- The paperless-ngx parser plugin runs in **every** Celery worker process. Bundling a Go shared library (`cgo` / `.so`) in a Python wheel would be a packaging nightmare across architectures and Python versions.
- HTTP keeps the deployment story identical to today: one container.
- Latency is irrelevant at the per-document timescale (seconds for any LLM call dwarfs HTTP overhead).
- The paperless-ngx shim stays trivially auditable (~200 LOC), which matters for code that loads into the user's paperless-ngx process.

## Proposed API surface (this repo)

A new versioned namespace under `/api/v1/` to avoid coupling with the existing UI-facing `/api/*` endpoints. **No paperless-ngx-specific fields.** Everything optional that maps to a paperless-ngx concept (mailrule_id etc.) is also useful in other contexts ("context tags" for routing) and stays generic in naming.

### `POST /api/v1/parse`

**Request** (multipart/form-data):

| field | type | description |
|---|---|---|
| `file` | binary | The document. Required. |
| `mime_type` | string | MIME type. Optional — sniffed if absent. |
| `filename` | string | Original filename. Optional, used for routing/logging. |
| `produce_archive` | bool | Generate a searchable PDF. Default `true`. |
| `produce_thumbnail` | bool | Generate a WebP thumbnail. Default `true`. |
| `produce_text` | bool | Extract plain text. Default `true`. |
| `provider` | string | Override the default OCR/LLM provider for this call. Optional. |
| `language_hint` | string | BCP-47 hint for the model. Optional. |
| `context` | string (JSON) | Free-form per-call context the caller wants echoed/used (e.g. `{"source": "paperless-ngx", "mailrule_id": 5}`). Opaque to the server unless a provider chooses to consume it. |

**Response** `200 application/json`:

```jsonc
{
  "text": "extracted full text…",
  "date": "2025-03-14",                       // ISO-8601, nullable
  "page_count": 4,                            // nullable
  "archive_pdf_b64": "JVBERi0xLjcK…",        // base64, nullable
  "thumbnail_webp_b64": "UklGRkQAAA…",       // base64, nullable
  "metadata": [
    {"namespace": "http://ns.adobe.com/pdf/1.3/", "prefix": "pdf", "key": "Producer", "value": "paperless-gpt 1.0"}
  ],
  "provider": "mistral_ocr",                  // which OCR provider handled it
  "ocr_limit_hit": false                      // surfaces ocr.OCRResult.OcrLimitHit
}
```

**Errors** (RFC 7807 `application/problem+json` recommended):

| status | meaning |
|---|---|
| `400` | Malformed request (missing file, bad multipart, invalid `context` JSON) |
| `415` | `mime_type` not supported by this paperless-gpt configuration → paperless-ngx shim returns `None` from `score()` so core falls back to Tesseract |
| `429` | Provider rate-limited |
| `503` | LLM provider unreachable / quota exceeded → caller should retry or fall back |
| `500` | Unexpected error |

### `GET /api/v1/capabilities`

Lets any consumer discover what the running instance can do without a per-call round trip. The paperless-ngx shim uses it to populate `supported_mime_types()` and `score()`; an n8n node uses it to populate dropdowns; a CLI uses it for `--help`.

```jsonc
{
  "name": "paperless-gpt",
  "version": "1.0.0",
  "supported_mime_types": {
    "application/pdf": ".pdf",
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/tiff": ".tiff"
  },
  "providers": [
    {"id": "mistral_ocr",  "display_name": "Mistral OCR",            "can_produce_archive": true},
    {"id": "google_docai", "display_name": "Google Document AI",    "can_produce_archive": true},
    {"id": "azure_docai",  "display_name": "Azure Document Intelligence", "can_produce_archive": true},
    {"id": "vision_llm",   "display_name": "Vision LLM (configured model)", "can_produce_archive": true}
  ],
  "default_provider": "mistral_ocr",
  "can_produce_archive": true,
  "requires_pdf_rendition": false,
  "default_score": 50
}
```

### `GET /api/v1/healthz`

Standard liveness probe. Returns `200` when the configured OCR provider can be initialised.

### Authentication

Optional bearer token via `Authorization: Bearer <token>`. Configured server-side with `PAPERLESS_GPT_API_TOKEN`. When unset, the API is open (suitable for trusted Docker networks). When set, all `/api/v1/*` calls require a valid token.

### CORS

Disabled by default. Enable with `PAPERLESS_GPT_API_CORS_ORIGINS=*` (or a CSV) so browser-based tools can call the API directly.

## Internal Go changes (sketch)

| Area | Change |
|---|---|
| New file `parser_api.go` | Handlers for `/api/v1/parse`, `/api/v1/capabilities`, `/api/v1/healthz`. |
| [`ocr.go`](../ocr.go) | Extract a stateless variant of `ProcessDocumentOCR` that takes `(bytes, mime)` instead of `(documentID)`. Today's flow downloads from paperless-ngx via `app.Client.Download…`; the new flow reuses the **same** provider invocation logic without that prelude. |
| [`ocr/provider.go`](../ocr/provider.go) | Extend `OCRResult` (or wrap it) with `ArchivePDF []byte`, `ThumbnailWebP []byte`, `PageCount int`, `DetectedDate *time.Time`. |
| New file `archive/pdf.go` | Compose searchable PDF from `hocr.HOCR` + page images using `gardar/ocrchestra/pkg/pdfocr` (already a dep). |
| [`main.go`](../main.go) | New flag `--mode=parser-server` that boots the HTTP server but skips paperless-ngx polling and the web UI. Default mode unchanged. |
| [`Dockerfile`](../Dockerfile) | Optionally add a slim variant `paperless-gpt:parser-sidecar` (no frontend assets). |

The intent is to **share** all OCR/LLM code paths between the existing tag-based flow and the new parser-plugin flow. The plugin path is just a different entry point.

## Reference consumers

To prove the API is genuinely consumer-agnostic, we plan to ship (or document) a small example for each persona alongside the main implementation. None of them are required for the API itself — they're proof points.

| Consumer | Form | Effort |
|---|---|---|
| **paperless-ngx parser plugin** | New repo `paperless-gpt-parser` (Python, ~200 LOC, on PyPI) | The headline use case. |
| **n8n** | Single "HTTP Request" node + a documented JSON template in the README | ~0 LOC, just docs. |
| **CLI** | `pgpt parse <file>` — small Go binary in `cmd/pgpt`, or a one-line `curl` snippet | ~50 LOC for a real CLI; 1 line for `curl`. |
| **Coding agents (MCP)** | Optional thin MCP server that exposes `parse_pdf` as a tool | Separate repo, can come later. |
| **OpenAPI** | Generate `openapi.yaml` from the handler types and ship it under `/api/v1/openapi.yaml` | Auto-discovery for any HTTP framework. |

## Sibling repo: `paperless-gpt-parser` (Python shim)

Will live in a new repo under the same org. Skeleton:

```
paperless-gpt-parser/
├── pyproject.toml
└── src/paperless_gpt_parser/
    ├── __init__.py
    ├── parser.py        # GptParser implementing ParserProtocol
    ├── client.py        # httpx client for paperless-gpt
    └── config.py        # PAPERLESS_GPT_URL, score override, MIME allow-list
```

Configuration (env vars read by the shim, *not* by paperless-ngx):

| Env | Default | Purpose |
|---|---|---|
| `PAPERLESS_GPT_URL` | `http://paperless-gpt:8080` | Base URL of sidecar |
| `PAPERLESS_GPT_PARSER_ENABLED` | `true` | Master kill-switch |
| `PAPERLESS_GPT_PARSER_SCORE` | `50` | Score returned when supported (Tesseract returns ~10) |
| `PAPERLESS_GPT_PARSER_TIMEOUT` | `300` | Per-document HTTP timeout (seconds) |
| `PAPERLESS_GPT_PARSER_MIME_ALLOW` | *all from `/capabilities`* | Optional CSV restriction |

The shim performs `GET /capabilities` once at import time (cached), and on every `parse()` it streams the file over `POST /api/v1/parse`, then writes `archive_pdf_b64` + `thumbnail_webp_b64` to its own temp directory so `get_archive_path()` and `get_thumbnail()` can return real `Path` objects to paperless-ngx.

## Migration & compatibility story

| paperless-ngx version | paperless-gpt deployment |
|---|---|
| ≤ 2.x | Existing tag-based polling — unchanged. |
| 3.0+ | Either: keep tag-based polling, **or** install `pip install paperless-gpt-parser` in the paperless-ngx container and run paperless-gpt as a sidecar. |
| Future (≥ 4.x?) | Parser plugin becomes the recommended path; tag polling deprecated but not removed for one major cycle. |

We never break existing users. That's a story no Python-only competitor can tell.

## Open questions for discussion

1. **Endpoint shape** — base64 in JSON vs. `multipart/mixed` response? JSON is dead simple; multipart saves ~33 % bandwidth. Likely irrelevant in practice.
2. **Provider selection per request** — should the shim be able to override `LLM_PROVIDER` per request (e.g. via header)? Helpful for users with a "cheap LLM for normal docs, expensive LLM for handwritten" rule.
3. **Streaming progress** — paperless-ngx gives us no way to surface progress mid-`parse()`. Acceptable?
4. **Archive PDF format** — paperless-ngx supports PDF/A archive output. We currently produce regular PDFs with hOCR layer. Do we need PDF/A conformance?
5. **Auth** — assume trusted network (Docker bridge) by default? Add optional shared-secret header for users running the sidecar on a different host?
6. **Should the shim live in this repo or its own?** Separate repo is cleaner for `pip install`; monorepo is easier to keep in sync. Leaning separate repo.
7. **Where does the existing UI fit?** The web UI for manual review/tagging stays useful even when the parser plugin runs automatically. The two flows can coexist: parser plugin handles ingestion, UI handles human-in-the-loop refinement.
8. **Replacing `extract_metadata`** — paperless-gpt's tag/title/correspondent suggestions don't fit the parser protocol's `MetadataEntry` shape (they update Django models, not file-level metadata). Those keep flowing through the existing tag-based path or via paperless-ngx workflows. Plugin scope is strictly ingestion-time text/PDF/thumbnail.

## Out of scope for this RFC

- LLM-based title/tag/correspondent suggestions — these are document-level, not parse-time, and continue via the existing flow.
- The web UI for manual OCR re-runs.
- Anything Frontend.

## Roadmap (proposed)

| Step | Status | Deliverable |
|---|---|---|
| 1 (this PR) | ✅ | RFC + working MVP: text extraction for images and PDFs, capabilities/healthz, optional bearer auth, Python shim skeleton, curl quickstart, docker-compose example. |
| 2 | 🚧 | Refactor the polling-flow `ProcessDocumentOCR` to share more code with the new stateless path (today they overlap but are not unified). |
| 3 | 🚧 | Searchable archive PDF in the `/parse` response (hOCR + page images via `gardar/ocrchestra/pkg/pdfocr`). |
| 4 | 🚧 | Real WebP thumbnail in the `/parse` response. |
| 5 | 🚧 | Per-request `provider` override + `language_hint` honoring. |
| 6 | 🚧 | Move `paperless-gpt-parser/` into its own repository, publish to PyPI, integration tests against `paperless-ngx:dev`. |
| 7 | 🚧 | Announcement back in [discussion #12023](https://github.com/paperless-ngx/paperless-ngx/discussions/12023). |

---

**Feedback wanted on:** API shape, deployment model, scope, naming, anything labeled "open question" above. Comment in this PR or in the linked paperless-ngx discussion.
