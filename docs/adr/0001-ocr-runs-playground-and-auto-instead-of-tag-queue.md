# 1. OCR Runs are persisted, re-runnable units; Playground + Auto-OCR instead of a manual tag queue

Date: 2026-07-12

## Status

Accepted

## Context

LLM-based OCR is the product's headline feature, but its UI consisted of one
page taking a raw document ID, and the powerful pipeline options (searchable
PDFs, replace-original, page limits) were invisible env-only switches. Auto-OCR
ran entirely outside any observable state — its outcomes existed only in logs.

An unused `MANUAL_OCR_TAG` env variable (marked "not used yet") pointed at a
third possible direction: a tag-driven manual OCR queue, mirroring the
metadata-suggestions flow.

The UI's guiding purpose (PRODUCT.md, principle 6) is to ramp users toward
hands-off automation: manual surfaces exist to build the trust and the
settings that let auto mode take over.

## Decision

1. **The core OCR interaction is a Playground plus a persisted Activity log —
   not a tag-driven queue.** The maintainer explicitly chose interactive
   single-document runs (pick any document, run, inspect page by page, tune
   the prompt, apply) plus visibility for unattended Auto-OCR. The
   `MANUAL_OCR_TAG` variable is removed; the suggestions flow remains the only
   tag-driven queue.

2. **Every OCR execution is an "OCR Run": a first-class, persisted record**
   (SQLite via the existing gorm database) carrying its trigger (manual/auto),
   run options, per-page results, status, timing, and the searchable-PDF
   outcome (attached / replaced / skipped / failed). Auto-OCR registers runs
   through the same mechanism, so one Activity log answers "what did OCR do,
   and did it delete any originals?" — the trust prerequisite for hands-off
   use, given that replace-original is irreversible.

3. **Run Options are per-run, with settings-persisted defaults overriding
   env values.** The env variables remain as the bootstrap layer; a
   `PUT /api/ocr/defaults` endpoint (the "save as defaults" ramp in the
   Playground) persists tuned options into `config/settings.json`, which
   Auto-OCR then uses. Page texts are retained for the last 5 runs per
   document (run comparison), run records capped at 1000.

## Consequences

- The SQLite schema gains `ocr_runs` and a `job_id` column on
  `ocr_page_results`; page results are no longer overwritten per document but
  scoped to runs and pruned.
- `PDF_UPLOAD`/`PDF_REPLACE`/`OCR_LIMIT_PAGES`/`OCR_PROCESS_MODE` become
  defaults rather than fixed behavior; UI-saved defaults take precedence.
  Setups that manage everything via env keep working unchanged until someone
  saves defaults from the UI.
- Searchable-PDF generation no longer requires `CREATE_LOCAL_PDF`; a run
  requesting an upload generates the PDF regardless.
- A future manual OCR queue would be a new decision; nothing in the run model
  blocks it, but there is deliberately no half-built tag flow in the codebase.
