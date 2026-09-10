# 2. Env registry as the structured source of truth for configuration

Date: 2026-07-13

## Status

Accepted

## Context

paperless-gpt reads ~80 environment variables, historically documented only in
a hand-maintained README table. The table already drifted from the code (vars
read but undocumented, vars documented but no longer read). A read-only
"Active configuration" diagnostics view (a support tool for hands-off
operators) needs structured, reliably-complete metadata per variable — a
Markdown table is not a usable API for that, and the README is not even present
in the binary.

The app has no authentication: anything an endpoint returns is readable by
anyone who can reach the UI.

## Decision

1. **`envRegistry` (env_registry.go) is the structured source of truth** for
   the config view: each entry has name, category, default, `secret` flag and
   description. A **drift test** walks the Go source and fails the build if any
   literal `os.Getenv`/`os.LookupEnv` key is missing from the registry, so the
   registry can never silently fall behind the code.

2. **`GET /api/config` is read-only and never exposes secret values.** Secret
   entries report only `is_set`; URL values have embedded `user:pass@`
   credentials scrubbed. The view reuses the OCR source model (`env` / `saved`
   / `default`) so UI-saved shadowing (ADR 0001 amendment) is visible here too.

3. **Changing values stays where it already lives** — environment/compose for
   deploy config, the linked in-app editors for the few runtime-editable ones.
   No new write path is introduced.

## Consequences

- Adding an `os.Getenv("NEW_VAR")` now requires a registry entry or the test
  fails — documentation of new settings is enforced, not hoped for.
- Two sources of truth still exist: the registry (code + UI) and the README
  table (humans browsing GitHub). **Follow-up (deferred):** generate the README
  env table from the registry via `go generate`, with a CI check that it is up
  to date. Deferred because it changes the contributor workflow (the table
  would no longer be hand-edited) and warrants its own change; the drift test
  already guarantees the *UI* is complete in the meantime.
- Dynamically-composed keys (e.g. `os.Getenv(prefix+"MAX_RETRIES")`) and keys
  consumed outside Go (`PUID`, `PGID`, `GOOGLE_APPLICATION_CREDENTIALS`) are in
  the registry but not caught by the drift scan; they are maintained by hand.
