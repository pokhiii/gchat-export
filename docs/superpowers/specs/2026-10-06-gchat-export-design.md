# gchat-export — Design Spec

- **Date:** 2026-10-06
- **Status:** Approved design, pending implementation plan
- **Author:** Abhishek Pokhriyal

## 1. Purpose

A local CLI that exports Google Chat conversations — for a chosen space (room, group chat, or DM) and time period — into files that are pleasant for humans to read and easy to feed into LLMs/analysis tools, optionally including attachments. The tool will be open-sourced, so security and supply-chain hygiene are first-class requirements.

### Success criteria

1. Given a space and a date range, the tool exports every message the authenticated user can see in `chat.google.com` for that range, including thread structure, sender names, timestamps, edits, reactions, and attachment references.
2. Output includes a readable Markdown transcript and a structured JSONL file.
3. Uploaded attachments can optionally be downloaded alongside the transcript.
4. No secrets are committed or shipped; tokens are stored in the OS keychain; only read-only scopes are requested.
5. Releases are reproducible, signed, and accompanied by checksums, SBOM, and provenance.

### Decisions (from brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| Data access | Official Google Chat API (user OAuth) | Supported, stable, ToS-compliant; avoids handling session cookies. |
| Account type | Google Workspace | Chat API user auth requires Workspace. |
| Language | Go | Single static binary, small dependency tree, official `google.golang.org/api/chat/v1` client, easy signed releases. |
| OAuth client | Bring-your-own (user's own GCP project, Desktop client, Internal app) | No secrets in repo; no Google verification needed; maintainer is not a trust anchor. |
| Outputs | Markdown + JSONL, optional attachments | Human reading/archival + LLM ingestion. |

### Non-goals (v1)

Bulk export of all spaces, live/incremental sync, HTML output, downloading Drive-hosted attachments (links only), search, any write operation, consumer (non-Workspace) Gmail accounts, admin/domain-wide delegation exports.

## 2. CLI surface

```
gchat-export auth login [--client-secret PATH] [--insecure-file-store]
gchat-export auth status
gchat-export auth logout
gchat-export spaces [--type space|dm|group] [--filter TEXT] [--json]
gchat-export export --space ID|DISPLAY_NAME
                    --since DATE|RFC3339 [--until DATE|RFC3339] [--tz ZONE]
                    [--format md,jsonl] [--attachments] [--max-attachment-size 100MB]
                    [--out DIR] [--resume] [--force] [--rps N] [--verbose]
```

Global configuration precedence: flag > environment variable (`GCHAT_EXPORT_*`, e.g. `GCHAT_EXPORT_CLIENT_SECRET`) > config file (`<os.UserConfigDir>/gchat-export/config.json`; JSON to avoid a TOML dependency).

`--space` accepts a resource name (`spaces/AAAA…`) or an exact display name. If a display name matches zero or multiple spaces, the command fails and lists candidates.

Defaults: `--until` = now; `--format md,jsonl`; `--out ./export`; `--rps 10`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Fatal error (auth, bad arguments, unrecoverable API error) |
| 2 | Partial success (export completed, some attachments/items failed; see `manifest.json`) |

## 3. Architecture

Single Go module, `github.com/<owner>/gchat-export`. The owner is set at open-sourcing; a placeholder module path is used until then.

```
cmd/gchat-export/main.go     # entrypoint
internal/cli/                # cobra commands, flag parsing, wiring only
internal/config/             # config resolution (flag/env/file)
internal/auth/               # OAuth loopback+PKCE flow, token store
internal/chat/               # Chat API wrapper: spaces, members, messages, media
internal/model/              # tool-owned domain types
internal/render/             # Markdown and JSONL writers
internal/attach/             # attachment download + path sanitization
internal/fsutil/             # safe file creation (perms, atomic write, no-follow)
internal/ratelimit/          # client-side token-bucket + retry/backoff transport
internal/daterange/          # --since/--until/--tz parsing into a [from, to) UTC range
internal/export/             # export orchestration: output dir, resume state, manifest
```

### Unit responsibilities

- **auth** — Loads the user's `client_secret.json` (Desktop type). Runs the loopback flow: binds `127.0.0.1:0`, generates `state` (32 random bytes) and PKCE S256 verifier, opens the browser (prints URL as fallback), accepts exactly one callback whose `state` matches, then shuts down; 2-minute timeout. Exchanges the code and persists the token via `TokenStore`. Exposes an `oauth2.TokenSource` that persists refreshed tokens. `logout` revokes the refresh token at `https://oauth2.googleapis.com/revoke` before deleting it locally.
- **TokenStore** (interface in auth) — v1 supports one signed-in account. `KeyringStore` (default; `github.com/zalando/go-keyring`, service `gchat-export`, key `default`, value = JSON record holding email, granted scopes, token) and `FileStore` (only with `--insecure-file-store`; file `0600` in a `0700` dir under the user config dir; refuses to read if perms are looser).
- **chat** — Thin wrapper over `google.golang.org/api/chat/v1`. Methods: `ListSpaces`, `GetSpace`, `ListMembers`, `ListMessages(space, from, to)` (iterator; filter `createTime >= "from" AND createTime < "to"` with RFC 3339 UTC; `orderBy createTime ASC`; `pageSize 1000`; `showDeleted false`), `DownloadMedia(resourceName, w io.Writer, limit int64)`. Converts API types to `model` types.
- **Name resolution** (in chat) — Sender names come from `sender.displayName`, which the Chat API populates under user auth. Fallback: membership `member.displayName` from the prefetched member list, then the raw `users/{id}`. No People API.
- **model** — `Space`, `User`, `Message` (name, thread name, sender, createTime, lastUpdateTime, text, formatted text, reactions summary, attachments, quoted message ref, deleted flag), `Attachment` (name, contentType, source: uploaded|drive, resource name or Drive URL), `Manifest`.
- **render** — JSONL is written per message (streaming). Markdown is rendered once at the end from the JSONL, so threads can be grouped correctly.
- **attach** — Downloads uploaded attachments via `chat.media.download` using `attachmentDataRef.resourceName`. Drive attachments recorded as links only. Computes SHA-256 while streaming.
- **fsutil** — `CreateExclusive(dir, name)` with `0600`, `MkdirAll` with `0700`, atomic write (temp file in same dir + `fsync` + rename), containment check, no-follow semantics.
- **ratelimit** — `http.RoundTripper` that applies a token bucket (`golang.org/x/time/rate`) and retry with exponential backoff + full jitter.

### Data flow (export)

1. Resolve config → load token (fail with "run `auth login`" if absent).
2. Resolve `--space` → `Space`.
3. Prefetch members (for name fallback).
4. Open output dir `export/<sanitized-space-name>/<from>_<to>/` (refuse if non-empty unless `--resume`/`--force`).
5. Iterate `ListMessages` page by page: if `--attachments`, download uploaded attachments → append JSONL lines → fsync → update `.state.json` (page token + JSONL byte offset).
6. Render `transcript.md` from `messages.jsonl` (JSONL is the source of truth; it is deleted at the end if `jsonl` was not requested).
7. Write `manifest.json` (space, range, counts, tool version, scopes used, attachment SHA-256s, `errors[]`); remove `.state.json`.

## 4. OAuth scopes

| Scope | When |
|---|---|
| `https://www.googleapis.com/auth/chat.spaces.readonly` | Always |
| `https://www.googleapis.com/auth/chat.messages.readonly` | Always (also authorizes `media.download`) |
| `https://www.googleapis.com/auth/chat.memberships.readonly` | Always |
| `openid`, `email` | Always — identify the signed-in account for `auth status` via tokeninfo (non-sensitive) |

No write scopes and no Drive scopes are ever requested. `auth status` prints granted scopes and the account email.

## 5. Output formats

Directory: `export/<space-slug>/<from>_<to>/` where `<from>`/`<to>` are `YYYY-MM-DD` (or compact RFC 3339 if times were given).

```
transcript.md
messages.jsonl
attachments/<message-id>_<sanitized-filename>
manifest.json
```

### Markdown

- Header: space name, type, export range with timezone, generated-at, message count.
- Messages grouped by day (`## 2026-09-03`), threads shown as a top-level message followed by indented replies (`>` blockquote).
- Each message: `**Sender** · 14:32` then text. Edited messages marked `(edited)`. Reactions as `👍 3 · 🎉 1`. Attachments as Markdown links to relative `attachments/…` paths or Drive URLs.
- Times rendered in `--tz` (default local).
- Escaping: raw HTML is escaped (`<`, `>`, `&`); links are emitted only for `http`, `https`, and relative attachment paths — any other scheme (e.g. `javascript:`, `data:`) is rendered as plain text.

### JSONL

One object per message, stable flat schema intended for LLM ingestion:

```json
{"id":"spaces/X/messages/Y","thread_id":"spaces/X/threads/Z","is_thread_reply":true,
 "time":"2026-09-03T09:02:11Z","edited_time":null,"sender_id":"users/123",
 "sender_name":"Jane Doe","text":"…","reactions":[{"emoji":"👍","count":3}],
 "attachments":[{"name":"report.pdf","content_type":"application/pdf","source":"uploaded","path":"attachments/Y_report.pdf","sha256":"…"}],
 "quoted_message_id":null}
```

## 6. Security design

### Threat model

**Assets:** OAuth refresh token (persistent read access to all of the user's Chat); exported conversation data.
**Adversaries:** other local users/processes, malicious message content/attachments authored by other chat members, accidental leakage via logs/shell history/git, compromised dependencies or CI.
**Out of scope:** an attacker who already controls the user's OS account; data retention after export; messages Google no longer returns.

### Controls

**Authentication & tokens**
- OAuth installed-app flow with PKCE (S256) and random `state`; listener bound to `127.0.0.1` only, random port, single-use, 2-minute timeout; redirect handler validates `state` and rejects anything else.
- Token stored in OS keychain by default. File fallback is opt-in (`--insecure-file-store`), `0600`/`0700`, refused if permissions are looser.
- `client_secret.json` is only referenced by path (flag/env/config) — never copied; warn if group/world-readable.
- Least privilege: three read-only Chat scopes plus `openid email`.
- `auth logout` revokes server-side before deleting locally.
- Tokens are never passed via flags or environment variables (avoids shell history / process listing leaks).

**Data handling**
- No secrets or message content in logs or error messages. `--verbose` logs API method names, counts, durations, HTTP status codes only. A redaction helper strips `Authorization` headers and token-shaped values from any wrapped error.
- Output files `0600`, directories `0700`; atomic writes.
- Attachments treated as untrusted:
  - Filename sanitization: strip path separators, `..`, control chars, leading dots/dashes; reject Windows reserved names (`CON`, `NUL`, `COM1`…); NFC-normalize; cap length at 200 bytes; prefix with message ID to avoid collisions.
  - Final path must resolve inside the attachments dir (`filepath.Rel` containment check).
  - Created with `O_CREATE|O_EXCL` and `O_NOFOLLOW` (Unix); on Windows, `Lstat` check immediately before create.
  - Size cap enforced while streaming (`io.LimitReader` + overflow detection); oversized files deleted and reported.
  - Never opened/executed; declared MIME type recorded, not trusted.
- Markdown injection mitigated by escaping and scheme allowlisting (see §5).
- No telemetry. Network egress only to `*.googleapis.com`, `oauth2.googleapis.com`, `accounts.google.com`.

**Repository & supply chain**
- Minimal dependencies: `golang.org/x/oauth2`, `google.golang.org/api`, `golang.org/x/time`, `github.com/spf13/cobra`, `github.com/zalando/go-keyring`. New dependencies require justification in the PR.
- `.gitignore` includes `client_secret*.json`, `token*.json`, `export/`, `.state.json`.
- CI: `go test -race`, `go vet`, `staticcheck`, `golangci-lint`, `gosec`, `govulncheck` on Linux/macOS/Windows. Nightly `govulncheck`. Optional pre-commit `gitleaks`.
- GitHub Actions pinned by commit SHA; `permissions:` minimal per job (default `contents: read`).
- Dependabot for `gomod` and `github-actions`.
- Releases via `goreleaser`: `-trimpath`, `CGO_ENABLED=0`, reproducible builds; SHA-256 checksums; keyless `cosign` signatures (Sigstore, GitHub OIDC); SLSA provenance; SBOM (syft/CycloneDX).
- `SECURITY.md` with GitHub private vulnerability reporting; branch protection on `main`; OpenSSF Scorecard workflow.
- Docs: `docs/setup.md` walks through creating a GCP project, enabling the Chat API, configuring the OAuth consent screen as **Internal**, creating a **Desktop** client, and what a Workspace admin may need to allow.

## 7. Error handling

- **Retryable** (429, 500, 502, 503, 504, network timeouts/resets): exponential backoff with full jitter, base 500 ms, cap 30 s, max 6 attempts; honor `Retry-After`.
- **Client-side rate limit:** token bucket, default 10 rps (`--rps`).
- **Non-retryable, actionable messages:**
  - 401 / `invalid_grant` → "Token expired or revoked; run `gchat-export auth login`."
  - 403 insufficient scope → names the missing scope; suggests re-login.
  - 403 app not allowed / API disabled → points to the relevant `docs/setup.md` section.
  - 404 space → suggests `gchat-export spaces --filter`.
- **Resume:** `.state.json` (page token, last message `createTime`, counts, partial attachment list) updated after each page. `--resume` continues from it; On resume, `messages.jsonl` is truncated to the byte offset recorded in state, so a page that was half-written is redone without duplicates.
- **Existing output:** non-empty target dir without `--resume` or `--force` → error. `--force` removes the dir contents first (only within the resolved output dir).
- **Partial failure:** individual attachment failures recorded in `manifest.json.errors[]`; exit code 2.
- **Time parsing:** `--since`/`--until` accept `YYYY-MM-DD` (interpreted at 00:00 in `--tz`, default local) or RFC 3339. Interval is `[since, until)`; converted to UTC for the API filter. `since >= until` is an argument error.

## 8. Testing strategy

- **Unit tests** per package using `httptest.Server` with recorded/fabricated Chat API JSON: pagination, filter string construction, retry/backoff (incl. `Retry-After`), sender-name fallbacks, time parsing/time zones.
- **Golden-file tests** for Markdown and JSONL rendering (threads, edits, reactions, attachments, multi-day, unicode).
- **Security tests:**
  - Fuzz (`go test -fuzz`) the filename sanitizer and containment check against traversal, absolute paths, reserved names, unicode tricks.
  - Symlink in attachments dir is not followed.
  - Created files/dirs have `0600`/`0700` (Unix).
  - Markdown escaping: HTML/`<script>`/`javascript:` payloads rendered inert.
  - Log capture during a full fake login + export contains no token or message text.
  - OAuth listener binds only to loopback, rejects wrong `state`, shuts down after one callback/timeout.
  - `FileStore` refuses world-readable token files.
- **E2E** behind `-tags e2e`, run manually against the developer's own account; never in CI; no CI secrets.
- **CI matrix:** ubuntu, macos, windows; latest stable Go.

## 9. Verified API facts (2026-10-06)

1. `media.download` (`GET /v1/media/{resourceName}?alt=media`) accepts `chat.messages.readonly`.
2. Under user auth, `User.displayName` is populated for message `sender` and for members in the Memberships API — People API not needed.
3. Read quotas: 3,000/min per project per method family; **15 reads/s per space** (includes `messages.list`, `members.list`, `media.download`). Default `--rps 10` stays under the per-space limit.
