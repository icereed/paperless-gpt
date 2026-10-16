# Document splitting

Tag a batch PDF with `paperless-gpt-split` and open **Split Suggestions**.
Review the page previews and edit the boundaries. Set `SPLIT_DRY_RUN=false` to
**Apply** a split or **Keep as one document** when no boundaries are selected.
**Skip** leaves the document on hold; **Keep** releases it for processing.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `SPLIT_TAG` | `paperless-gpt-split` | Queue tag; must differ from processing and workflow tags. |
| `SPLIT_DRY_RUN` | `true` | Generate suggestions; disable Apply and Keep. |
| `SPLIT_AUTO` | `false` | Automatically apply eligible new background suggestions. |
| `SPLIT_CONFIDENCE_THRESHOLD` | `0.9` | Minimum confidence for every automatic boundary. |
| `SPLIT_DELETE_ORIGINALS` | `false` | Delete originals after child documents are consumed. |
| `SPLIT_WINDOW_SIZE` | `8` | Pages per LLM request; minimum 3, with two-page overlap. |
| `SPLIT_OUTPUT_TAGS` | Effective `AUTO_TAG` | Tags for children or kept documents; cannot include `SPLIT_TAG`. |

Uses the configured LLM and `prompts/split_prompt.tmpl`. Existing prompt files
are preserved on upgrade. Tags are created at startup. Preserve `db/` and
`prompts/`, and run one instance for this queue.

## Processing

Detection reads the archive PDF's OCR text; splitting uses the original PDF.
Every page is retained. Pagination conflicts, overlapping-window disagreements
and boundaries beside pages with fewer than 10 OCR characters require review.
Manual Apply can override these flags.

Automatic splitting requires dry-run off, at least one boundary and every
boundary meeting the confidence threshold without conflicts. Existing, skipped
or regenerated suggestions never apply automatically.

Split sources stay out of background OCR and metadata processing unless kept
whole. Children inherit output tags; temporary output tags are removed from
retained originals. Keep removes the split tag and adds output tags.

## Recovery

- **Changed source:** Generate again; Apply checks checksums and page count.
- **Failed:** Generate again after fixing the error. Generation failures attempt to add `FAIL_TAG`.
- **Lost response:** Refresh before taking another action.
- **Submitted:** Check Paperless for the children; submission does not confirm consumption.
- **Needs inspection:** Inspect Paperless documents and task logs. Interrupted or uncertain splits cannot be resubmitted through Apply.

Paperless handles child consumption and original deletion asynchronously.
[Paperless split implementation](https://github.com/paperless-ngx/paperless-ngx/blob/v3.1.3/src/documents/bulk_edit.py#L735).

## Test your PDFs

Use a test instance with `SPLIT_DRY_RUN=true` and `PAPERLESS_API_TOKEN` set.
Each input PDF must contain one document:

```sh
go run ./cmd/split-eval -input /path/to/single-document-pdfs \
  -paperless-url http://localhost:8000 -gpt-url http://localhost:8080 \
  -batch-size 3 -timeout 20m
```

The command joins PDFs, uploads batches and scores suggested boundaries.
It never applies splits. Uploaded documents and suggestions remain on the test instance.
