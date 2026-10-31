# 3. Forward auth via the paperless-ngx login

Date: 2026-10-31

## Status

Accepted

## Context

paperless-gpt has no authentication (see ADR 0002): anyone who can reach the
port can read every document its `PAPERLESS_API_TOKEN` sees, rewrite metadata
and spend LLM credits. The README tells operators to put an authenticating
reverse proxy in front of it; issue #293 asks for built-in protection.

paperless-gpt almost always runs next to a paperless-ngx instance that already
has accounts, sessions, MFA and SSO. Its users are paperless-ngx users.

## Decision

1. **No accounts of its own.** With `PAPERLESS_FORWARD_AUTH` set, a gin
   middleware forwards the visitor's `Cookie` and `Authorization` headers to
   paperless-ngx and asks who they belong to. paperless-gpt's own token is
   never sent along. Default is `off`, so existing setups don't change.

2. **Levels: `superuser` and `user`.** paperless-gpt shows everything its own
   token sees, normally a superuser's. Any account with less can read
   documents through paperless-gpt that paperless-ngx hides from it.
   `superuser` is therefore the recommended level; `user` is for instances
   where every account may see everything anyway. There is no `staff` level:
   the staff flag grants no access to other users' documents, so it would be
   the same leak with a more reassuring name.

3. **`/api/ui_settings/`, with `/api/profile/` as fallback.** ui_settings is
   the only endpoint that reports `is_superuser` for the caller, but it needs
   the `view_uisettings` permission. On its 403, `/api/profile/` (login only)
   decides between "signed in, no superuser" and "not signed in". A 403 alone
   never counts as signed in, so a change in paperless-ngx' auth setup can't
   let anonymous visitors in at `user` level. The profile body (it contains
   the user's API token) is not read.

4. **Short cache, shared checks.** Results are cached for 5 s per credential
   (hashed key), concurrent checks for the same credentials are merged. The UI
   fires several API calls per page; a logout or a revoked superuser status
   still takes effect within seconds. Errors are not cached.

5. **CSRF protection only together with forward auth.** Browsers send the
   session cookie with cross-site requests too, so writes must come from
   paperless-gpt's own pages (`http.CrossOriginProtection`: `Sec-Fetch-Site`,
   falling back to `Origin` vs `Host`). Without forward auth no cookie grants
   anything, and enabling the check for everyone could break existing
   cross-origin setups; that is a separate decision.

6. **Browser flow.** Pages redirect unauthenticated visitors to
   `PAPERLESS_PUBLIC_URL/accounts/login/?next=…` (`X-Forwarded-Prefix` is
   honoured for sub-path setups); the API answers `401` with a `login_url`,
   which the web app follows when a session expires. Accounts without the
   required status get a static "access denied" page or a `403`.

## Consequences

- The session cookie only reaches paperless-gpt on the same host name as
  paperless-ngx (typically paperless-gpt under a sub-path behind the same
  reverse proxy). Other setups can still use API tokens or keep a proxy.
- Every cache miss costs one or two requests to paperless-ngx.
- Health checks against paperless-gpt's pages or API fail once forward auth is
  on; a TCP check is needed.
- The access-denied page is English only.
