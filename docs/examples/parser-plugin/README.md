# Quickstart: try the v1 parse API with curl

> Status: MVP (text only). See [`../parser_plugin_rfc.md`](../parser_plugin_rfc.md)
> for the full design and [PR #964](https://github.com/icereed/paperless-gpt/pull/964)
> for the discussion.

## 0. Run paperless-gpt with any OCR provider configured

The simplest local setup for trying the new endpoint:

```bash
# In one terminal — start paperless-gpt with a real OCR provider.
# Replace OPENAI_API_KEY etc. with whatever provider you have.
docker run --rm -it -p 8080:8080 \
  -e LLM_PROVIDER=openai \
  -e LLM_MODEL=gpt-4o-mini \
  -e VISION_LLM_PROVIDER=openai \
  -e VISION_LLM_MODEL=gpt-4o-mini \
  -e OCR_PROVIDER=llm \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e PAPERLESS_BASE_URL=http://example.invalid \
  -e PAPERLESS_API_TOKEN=anything \
  ghcr.io/icereed/paperless-gpt:pr-964
```

> Until a release of this branch is published, `docker build .` from the
> repo root and `docker run … paperless-gpt:local`.

## 1. Capabilities

```bash
curl -s http://localhost:8080/api/v1/capabilities | jq
```

```jsonc
{
  "name": "paperless-gpt",
  "version": "devVersion",
  "supported_mime_types": {
    "application/pdf": ".pdf",
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/tiff": ".tiff",
    "image/webp": ".webp"
  },
  "providers": [{ "id": "llm", "display_name": "llm", "can_produce_archive": false }],
  "default_provider": "llm",
  "can_produce_archive": false,
  "requires_pdf_rendition": false,
  "default_score": 50,
  "notes": [
    "MVP: text extraction only. Archive PDF and thumbnail are not yet returned.",
    "See docs/parser_plugin_rfc.md for the planned full surface."
  ]
}
```

## 2. Health

```bash
curl -s http://localhost:8080/api/v1/healthz
# {"status":"ok"}
```

## 3. Parse a PDF

```bash
curl -s -X POST http://localhost:8080/api/v1/parse \
  -F file=@./scan.pdf \
  -F mime_type=application/pdf \
  -F filename=scan.pdf | jq
```

```jsonc
{
  "text": "Invoice no. 12345 …",
  "page_count": 3,
  "provider": "llm"
}
```

## 4. Parse an image

```bash
curl -s -X POST http://localhost:8080/api/v1/parse \
  -F file=@./receipt.jpg \
  -F mime_type=image/jpeg | jq -r '.text'
```

## 5. Optional auth

Start the sidecar with `PAPERLESS_GPT_API_TOKEN=hunter2` and add a header:

```bash
curl -H 'Authorization: Bearer hunter2' \
     http://localhost:8080/api/v1/healthz
```

## 6. Use from n8n / Make / Zapier

It's a plain `multipart/form-data` POST. Any HTTP node works. The response
is JSON; pipe `text` into your downstream nodes.

## 7. Use from a coding agent

Have your agent call:

```bash
curl -s -X POST $PAPERLESS_GPT_URL/api/v1/parse \
  -F file=@"$1" \
  -F mime_type=$(file --mime-type -b "$1") \
  | jq -r '.text'
```

…and feed the resulting text back into context. Solves the "agent struggles
with PDFs in user prompts" problem in three lines.
