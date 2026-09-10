# paperless-gpt

AI companion for paperless-ngx: generates document metadata suggestions and runs
LLM-based OCR. Users review what the AI proposes and decide what gets written back.

## Language

### Suggestions (metadata flow)

**Suggestion**:
An AI-proposed value for one metadata field (title, tags, correspondent, document type, created date, custom fields) of one document, awaiting a user decision.
_Avoid_: recommendation, proposal

**Suggestion Queue**:
The set of documents carrying the filter tag in paperless-ngx, waiting for suggestion generation and review.
_Avoid_: inbox, backlog

**Suggestion Job**:
One asynchronous generation pass over a batch of selected documents, with per-document progress and per-document failures.

**Review**:
The step where a user inspects field diffs (current vs. suggested), edits, and decides per field and per document.

**Apply**:
Writing accepted changes to paperless-ngx. Applying also removes the filter tag, so the document leaves the Suggestion Queue. Every applied field change is recorded and undoable.
_Avoid_: save, submit, commit

**Skip**:
A per-document decision to apply nothing; the document keeps its filter tag and stays in the Suggestion Queue.

**Modification**:
One recorded field change on one document (previous value → new value), reversible from the History page.
_Avoid_: change log entry, audit record

### OCR

**OCR Run**:
One execution of OCR for one document with a specific set of run options; it has a trigger, a status, per-page results, and is recorded in the OCR Activity log. Runs can be repeated with adjusted options.
_Avoid_: OCR job (reserve "job" for the queue/worker mechanics), scan

**Run Options**:
The per-run settings of an OCR Run: page limit, process mode, searchable-PDF handling, metadata copying, and an optional prompt override.
_Avoid_: config, settings (those are the global env-derived defaults)

**Trigger**:
What started an OCR Run: *manual* (a user in the Playground) or *auto* (the background loop via the auto-OCR tag).

**Playground**:
The interactive page where a user picks any document, starts OCR Runs, inspects per-page results next to the scan, tunes the prompt, and applies results.
_Avoid_: experimental OCR, OCR page

**Prompt Override**:
A temporary, run-scoped edit of the OCR prompt template; it never changes the globally saved template.

**Searchable PDF**:
A generated PDF with an invisible text layer over the original scan. It is either **attached** (uploaded as a new document) or **replaces** the original (the original is deleted — permanent, never undoable).
_Avoid_: OCR PDF, text-layer PDF

**Auto-OCR**:
The unattended background loop that runs OCR on every document carrying the auto-OCR tag, using the global defaults as Run Options.

**OCR Activity**:
The persisted history of OCR Runs (manual and auto) — status, pages, duration, failures, and whether a Searchable PDF was attached or replaced. Entry point for re-running with adjusted Run Options.
_Avoid_: job list, log

### Configuration

**Env Registry**:
The single structured source of truth for every environment variable paperless-gpt understands (name, category, default, secret flag, meaning). A drift test fails the build if code reads a variable missing from it.

**Active Configuration**:
The read-only diagnostics view of every setting's effective value, its Source, and its meaning. Answers "what is this instance actually running with?". Secrets are shown only as set / not set.
_Avoid_: settings dump, env dump

**Source**:
Where a setting's effective value comes from: `env` (set in the environment), `saved` (a UI-saved value shadows the environment) or `default`. Saved shadowing is always surfaced so the environment never silently lies.
