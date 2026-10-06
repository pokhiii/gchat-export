# gchat-export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go CLI that exports a Google Chat space's messages for a date range into Markdown + JSONL (plus optional attachments) using the official Chat API with user OAuth.

**Architecture:** Small `internal/` packages with one job each: `fsutil` (safe files), `daterange`, `ratelimit` (HTTP transport), `auth` (PKCE loopback login + keychain token store), `chat` (API wrapper → tool-owned `model` types), `attach`, `render`, `export` (orchestration/resume/manifest), and `cli` (cobra wiring). JSONL is the source of truth written page-by-page; Markdown is rendered from it at the end.

**Tech Stack:** Go (stdlib `log/slog`, `net/http/httptest`), `golang.org/x/oauth2`, `google.golang.org/api/chat/v1`, `golang.org/x/time/rate`, `golang.org/x/text/unicode/norm`, `github.com/spf13/cobra`, `github.com/zalando/go-keyring`.

**Spec:** `docs/superpowers/specs/2026-10-06-gchat-export-design.md`

## Global Constraints

- Module path: `github.com/OWNER/gchat-export` (placeholder; replaced at open-sourcing). Binary: `gchat-export`.
- Go: `go` directive = version installed in Task 1, minimum `1.25`. `CGO_ENABLED=0`.
- Allowed third-party deps: only those in Tech Stack (+ their transitive deps). Any other dep needs a justification line in the commit message.
- OAuth scopes (exact): `https://www.googleapis.com/auth/chat.spaces.readonly`, `https://www.googleapis.com/auth/chat.messages.readonly`, `https://www.googleapis.com/auth/chat.memberships.readonly`, `openid`, `email`. Never write or Drive scopes.
- Files written by the tool: `0600`; directories: `0700`. Never follow symlinks when writing.
- Never log/print token values, client secrets, or message text. `--verbose` logs method names, counts, durations, HTTP status only.
- Never pass tokens via flags or env vars. Never put tokens in URLs.
- Network egress only to `*.googleapis.com`, `oauth2.googleapis.com`, `accounts.google.com`.
- Retry: statuses 429, 500, 502, 503, 504 + network timeouts; base 500 ms, cap 30 s, full jitter, max 6 attempts; honor `Retry-After` (capped at 60 s). Default `--rps 10`.
- Exit codes: 0 success, 1 fatal, 2 partial.
- Defaults: `--until` now, `--format md,jsonl`, `--out ./export`, `--max-attachment-size 100MB`.
- Commits are SSH-signed with a passphrase: implementers **stage** and hand the `git commit` command to the human; never disable signing.

## Review Focus

1. **DMs and unnamed group chats have empty `displayName`** → output dir slug must fall back to `dm-<spaceID>`/`group-<spaceID>`, Markdown header must still be meaningful. (Test in Task 11.)
2. **Thread reply whose root is before `--since`** → reply must still appear, rendered as a top-level entry marked `↳ reply in thread`, not dropped or crashed. (Test in Task 9.)
3. **Attachment name collisions / empty names** (two `image.png` in one message, name `""` or `"..."`) → distinct, valid files, none overwritten. (Test in Task 10.)
4. **Interrupted export resumed after attachment files of the uncommitted page were partially written** → resume replaces them, no duplicate JSONL lines. (Test in Task 11.)
5. **Date-only range across a DST change / non-UTC `--tz`** → `--since 2026-03-08 --tz America/New_York` starts at local midnight, not UTC midnight. (Test in Task 3.)

---

### Task 1: Toolchain, module skeleton, and `fsutil`

**Files:**
- Create: `go.mod`, `cmd/gchat-export/main.go` (prints version, exits 0), `internal/fsutil/fsutil.go`, `internal/fsutil/nofollow_unix.go` (`//go:build !windows`), `internal/fsutil/nofollow_windows.go`, `internal/fsutil/sanitize.go`
- Test: `internal/fsutil/fsutil_test.go`, `internal/fsutil/sanitize_test.go`

**Interfaces:**
- Produces:
  - `func MkdirPrivate(path string) error` — `MkdirAll` 0700; if it exists, `Chmod` 0700.
  - `func CheckPrivate(path string) error` — returns `ErrInsecurePerms` if `mode&0o077 != 0` (Unix); nil on Windows.
  - `func Contained(root, path string) error` — `ErrEscapesRoot` unless `filepath.Rel(root, path)` stays inside root after `filepath.Clean`.
  - `func CreateNew(root, rel string) (*os.File, error)` — containment check; `O_WRONLY|O_CREATE|O_EXCL|O_NOFOLLOW`, 0600 (Windows: `Lstat` must return not-exist, then `O_EXCL`).
  - `func OpenAppend(root, rel string) (*os.File, error)` — same checks, `O_APPEND|O_CREATE`, refuses if the existing file is a symlink.
  - `func WriteFileAtomic(root, rel string, data []byte) error` — temp file in same dir (0600), write, `Sync`, `Rename`.
  - `func RemoveRegular(root, rel string) error` — `Lstat`; only removes regular files; `ErrNotRegular` otherwise.
  - `func SanitizeName(name string) string` — algorithm below.
  - Errors: `ErrInsecurePerms`, `ErrEscapesRoot`, `ErrNotRegular`.

- [ ] **Step 1: Install toolchain (ask the human first)** — `brew install go`, then `go install golang.org/x/vuln/cmd/govulncheck@latest honnef.co/go/tools/cmd/staticcheck@latest github.com/securego/gosec/v2/cmd/gosec@latest`. Verify `go version` ≥ 1.25.
- [ ] **Step 2: `go mod init github.com/OWNER/gchat-export`**, add `main.go` with `var version = "dev"`.
- [ ] **Step 3: Write failing tests**

```go
func TestCreateNewPerms(t *testing.T)            // file mode == 0600 (skip on windows)
func TestMkdirPrivateTightensExisting(t *testing.T) // pre-create 0755 → becomes 0700
func TestCreateNewRefusesSymlink(t *testing.T)   // root/a -> /tmp/x symlink; CreateNew(root,"a") errors; /tmp/x untouched
func TestCreateNewRefusesEscape(t *testing.T)    // rel "../x", "/etc/x" → errors.Is(err, ErrEscapesRoot)
func TestOpenAppendRefusesSymlink(t *testing.T)
func TestWriteFileAtomicReplaces(t *testing.T)   // existing content replaced; no *.tmp left; mode 0600
func TestCheckPrivate(t *testing.T)              // 0644 → ErrInsecurePerms; 0600 → nil
func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"report.pdf": "report.pdf", "../../etc/passwd": "etc_passwd", "a/b\\c": "a_b_c",
		"CON": "_CON", "nul.txt": "_nul.txt", ".bashrc": "bashrc", "-rf": "rf",
		"": "file", "...": "file", "a\x00b\x1fc": "abc", "é.txt": "é.txt",
	}
	// plus: 300-byte name → len(out) <= 200 and keeps extension; output never contains '/', '\\', ".."
}
func FuzzSanitizeName(f *testing.F) // invariant: out != "", no separators, no "..", len<=200, Contained(root, root+"/"+out)==nil
```

- [ ] **Step 4: Run** `go test ./internal/fsutil/...` → FAIL (undefined).
- [ ] **Step 5: Implement.** `SanitizeName` algorithm: NFC-normalize (`norm.NFC.String`); drop control chars; replace `/`, `\`, `:` with `_`; collapse `..` runs to `_`; trim leading `.`, `-`, `_`, spaces and trailing `.`/spaces; if base (before first `.`) case-insensitively matches `CON PRN AUX NUL COM1-9 LPT1-9`, prefix `_`; truncate to 200 bytes on a rune boundary preserving the last extension (≤16 bytes); empty → `"file"`.
- [ ] **Step 6: Run** `go test ./internal/fsutil/... && go test -fuzz=FuzzSanitizeName -fuzztime=30s ./internal/fsutil/` → PASS.
- [ ] **Step 7: Stage & hand off commit** — `git add go.mod cmd internal/fsutil && git commit -m "feat: module skeleton and safe filesystem helpers"`

---

### Task 2: Domain model

**Files:**
- Create: `internal/model/model.go`

**Interfaces:**
- Produces:

```go
type SpaceType string
const (SpaceTypeSpace SpaceType = "SPACE"; SpaceTypeGroup = "GROUP_CHAT"; SpaceTypeDM = "DIRECT_MESSAGE")
type Space struct { Name, DisplayName string; Type SpaceType }        // Name = "spaces/AAAA"
func (s Space) ID() string                                           // "AAAA"
type User struct { ID, DisplayName string }                          // ID = "users/123"
type Reaction struct { Emoji string `json:"emoji"`; Count int64 `json:"count"` }
type AttachmentSource string
const (SourceUploaded AttachmentSource = "uploaded"; SourceDrive = "drive")
type Attachment struct {
	Name, ContentType string; Source AttachmentSource
	ResourceName string // uploaded: attachmentDataRef.resourceName
	DriveURL     string // drive: https://drive.google.com/open?id=<driveFileId>
	Path, SHA256 string; Size int64 // set after download; Path relative to export dir
}
type Message struct {
	ID, ThreadID string; IsThreadReply bool
	Time time.Time; EditedTime *time.Time
	Sender User; Text string
	Reactions []Reaction; Attachments []Attachment
	QuotedMessageID string
}
func (m Message) ShortID() string // last path segment of ID
```

No tests of its own (pure types; `ID()`/`ShortID()` covered via later tasks). Fold into Task 3's commit.

---

### Task 3: `daterange`

**Files:**
- Create: `internal/daterange/daterange.go`
- Test: `internal/daterange/daterange_test.go`

**Interfaces:**
- Produces:
  - `type Range struct { From, To time.Time; Loc *time.Location; DateOnly bool }` — `From`/`To` in UTC; interval `[From, To)`.
  - `func Parse(since, until string, loc *time.Location, now time.Time) (Range, error)` — each value is `YYYY-MM-DD` (midnight in `loc`) or RFC 3339; empty `until` → `now`; `since` required; `From >= To` → `ErrEmptyRange`. `DateOnly` true iff both given values were dates (empty `until` counts as not date-only).
  - `func (r Range) DirName() string` — DateOnly: `2026-09-01_2026-10-01` (dates in `Loc`); else `20260901T093000Z_20261001T000000Z` (UTC).

- [ ] **Step 1: Write failing tests**

```go
func TestParseDateOnlyLocal(t *testing.T) // loc=Asia/Kolkata, "2026-09-01","2026-10-01" → From 2026-08-31T18:30:00Z, To 2026-09-30T18:30:00Z, DateOnly
func TestParseDSTStart(t *testing.T)      // Review Focus 5: loc=America/New_York since "2026-03-08" → From 2026-03-08T05:00:00Z; until "2026-03-09" → 2026-03-09T04:00:00Z
func TestParseRFC3339(t *testing.T)       // "2026-09-01T10:00:00+02:00" → From 08:00Z; DateOnly false
func TestParseUntilDefaultsNow(t *testing.T)
func TestParseRejects(t *testing.T)       // since=="" ; "2026-13-01"; "yesterday"; since>=until → errors.Is(err, ErrEmptyRange) for the last
func TestDirName(t *testing.T)            // both formats above
```

- [ ] **Step 2: Run** `go test ./internal/daterange/` → FAIL.
- [ ] **Step 3: Implement** with `time.ParseInLocation("2006-01-02", s, loc)` then `time.Parse(time.RFC3339, s)`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Stage & hand off** — `git add internal/model internal/daterange && git commit -m "feat: domain model and date range parsing"`

---

### Task 4: `ratelimit` transport

**Files:**
- Create: `internal/ratelimit/transport.go`
- Test: `internal/ratelimit/transport_test.go`

**Interfaces:**
- Produces:
  - `func NewTransport(base http.RoundTripper, rps float64, opts ...Option) http.RoundTripper`
  - `type Option func(*transport)`; `WithSleep(func(ctx context.Context, d time.Duration) error)`, `WithRand(func() float64)` (for deterministic tests).
  - Retries only `GET`/`HEAD` (all Chat reads are GET). Drains and closes discarded response bodies.

- [ ] **Step 1: Write failing tests** (against `httptest.Server`, sleep func records durations)

```go
func TestRetriesOn503ThenSucceeds(t *testing.T)   // 503,503,200 → 200; 3 requests
func TestGivesUpAfter6Attempts(t *testing.T)      // always 500 → returns last 500 response; exactly 6 requests
func TestNoRetryOn400And403(t *testing.T)         // 1 request each
func TestNoRetryOnPOST(t *testing.T)
func TestHonorsRetryAfterSeconds(t *testing.T)    // 429 + "Retry-After: 7" → slept 7s
func TestRetryAfterCapped(t *testing.T)           // "Retry-After: 3600" → slept 60s
func TestBackoffFullJitterBounds(t *testing.T)    // rand=1.0 → sleeps 500ms,1s,2s,4s,8s (≤30s cap); rand=0 → 0
func TestContextCancelStopsRetry(t *testing.T)
func TestRateLimitApplied(t *testing.T)           // rps=2, burst 1: 3 requests take ≥ ~1s (use real clock, tolerance)
```

- [ ] **Step 2: Run** `go test ./internal/ratelimit/` → FAIL.
- [ ] **Step 3: Implement** with `rate.NewLimiter(rate.Limit(rps), 1)`; `limiter.Wait(req.Context())` before each attempt; backoff = `rand() * min(30s, 500ms * 2^attempt)`.
- [ ] **Step 4: Run** `go test -race ./internal/ratelimit/` → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: rate-limited retrying HTTP transport"`

---

### Task 5: Token stores

**Files:**
- Create: `internal/auth/store.go`, `internal/auth/store_keyring.go`, `internal/auth/store_file.go`
- Test: `internal/auth/store_test.go`

**Interfaces:**
- Consumes: `fsutil.MkdirPrivate`, `fsutil.CheckPrivate`, `fsutil.WriteFileAtomic`, `fsutil.RemoveRegular`.
- Produces:

```go
type Credential struct { Email string `json:"email"`; Scopes []string `json:"scopes"`; Token *oauth2.Token `json:"token"` }
type Store interface { Load() (*Credential, error); Save(*Credential) error; Delete() error }
var ErrNoCredential = errors.New("not logged in")
func NewKeyringStore() Store           // service "gchat-export", user "default"; value = JSON(Credential)
func NewFileStore(dir string) Store    // dir/credential.json
```

v1 supports one signed-in account (spec §3 TokenStore note: key is fixed `default`; the email lives inside the record).

- [ ] **Step 1: Write failing tests**

```go
func TestKeyringRoundTrip(t *testing.T)       // keyring.MockInit(); Save→Load equal; Delete→Load ErrNoCredential
func TestFileStoreRoundTrip(t *testing.T)     // file mode 0600, dir 0700
func TestFileStoreRefusesLoosePerms(t *testing.T) // chmod 0644 → Load returns error wrapping fsutil.ErrInsecurePerms (skip windows)
func TestFileStoreMissing(t *testing.T)       // ErrNoCredential
```

- [ ] **Step 2: Run** `go test ./internal/auth/ -run Store` → FAIL.
- [ ] **Step 3: Implement.** Keyring `ErrNotFound` → `ErrNoCredential`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: keychain and file credential stores"`

---

### Task 6: OAuth login, token info, revoke, persisting token source

**Files:**
- Create: `internal/auth/login.go`, `internal/auth/google.go`, `internal/auth/tokensource.go`
- Test: `internal/auth/login_test.go`, `internal/auth/google_test.go`, `internal/auth/tokensource_test.go`

**Interfaces:**
- Consumes: `Store`, `Credential` (Task 5).
- Produces:

```go
var Scopes = []string{ /* exact list from Global Constraints */ }
func LoadClientConfig(path string) (*oauth2.Config, error) // google.ConfigFromJSON(data, Scopes...); requires "installed" client type
func Login(ctx context.Context, cfg *oauth2.Config, openBrowser func(url string) error, timeout time.Duration) (*oauth2.Token, error)
type Endpoints struct { TokenInfo, Revoke string } // defaults: https://oauth2.googleapis.com/tokeninfo, https://oauth2.googleapis.com/revoke
var DefaultEndpoints Endpoints
func FetchTokenInfo(ctx context.Context, hc *http.Client, ep Endpoints, accessToken string) (email string, scopes []string, err error) // POST form access_token=…
func Revoke(ctx context.Context, hc *http.Client, ep Endpoints, token string) error                                              // POST form token=…
func PersistingTokenSource(ctx context.Context, cfg *oauth2.Config, cred *Credential, store Store) oauth2.TokenSource                // saves when AccessToken changes
```

`Login` behavior: `net.Listen("tcp", "127.0.0.1:0")`; `RedirectURL = http://127.0.0.1:<port>/callback`; `state` = 32 bytes `crypto/rand` base64url; `verifier := oauth2.GenerateVerifier()`; auth URL with `oauth2.AccessTypeOffline`, `oauth2.SetAuthURLParam("prompt","consent")`, `oauth2.S256ChallengeOption(verifier)`; handler accepts only path `/callback`, only first request with matching `state`, responds with a static plain-text page ("You can close this tab"), never echoes query values; then `Exchange(ctx, code, oauth2.VerifierOption(verifier))`; server shut down on success, error, or `timeout`. If `openBrowser` errors, print the URL to stderr and keep waiting.

- [ ] **Step 1: Write failing tests** (fake token endpoint via `httptest`; `openBrowser` stub parses the auth URL and drives the callback)

```go
func TestLoginHappyPath(t *testing.T)           // token endpoint receives code_verifier whose S256 == code_challenge from auth URL; returns token
func TestLoginBindsLoopbackOnly(t *testing.T)   // redirect_uri host == "127.0.0.1"
func TestLoginRejectsWrongState(t *testing.T)   // stub calls callback with bad state → 400; then correct state still succeeds
func TestLoginTimeout(t *testing.T)             // stub does nothing, timeout 100ms → error; listener closed (dial fails)
func TestLoginAuthURLParams(t *testing.T)       // access_type=offline, prompt=consent, code_challenge_method=S256, scope contains all 5 scopes
func TestFetchTokenInfoUsesPOSTBody(t *testing.T) // server asserts method POST, token absent from URL; parses email & space-separated scope
func TestRevokePOST(t *testing.T)               // same URL/body assertions; non-200 → error
func TestPersistingTokenSourceSavesOnRefresh(t *testing.T)
func TestLoadClientConfigRejectsWebClient(t *testing.T) // JSON with "web" key → error
```

- [ ] **Step 2: Run** `go test ./internal/auth/` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/auth/` → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: OAuth loopback login with PKCE, revoke, token info"`

---

### Task 7: Chat API wrapper

**Files:**
- Create: `internal/chat/client.go`, `internal/chat/convert.go`, `internal/chat/errors.go`
- Test: `internal/chat/client_test.go`, `internal/chat/errors_test.go`, `internal/chat/testdata/*.json`

**Interfaces:**
- Consumes: `model.*`, `daterange.Range`.
- Produces:

```go
type Client struct { /* *chat.Service */ }
func New(ctx context.Context, hc *http.Client, endpoint string) (*Client, error) // endpoint "" = default; tests pass httptest URL
func (c *Client) ListSpaces(ctx context.Context) ([]model.Space, error)
func (c *Client) ResolveSpace(ctx context.Context, ref string) (model.Space, error) // "spaces/…" → GetSpace; else exact DisplayName match
func (c *Client) ListMembers(ctx context.Context, space string) (map[string]string, error) // users/ID → displayName
func (c *Client) ListMessages(ctx context.Context, space string, r daterange.Range, pageToken string, members map[string]string,
	fn func(page []model.Message, nextPageToken string) error) error
func (c *Client) DownloadMedia(ctx context.Context, resourceName string) (io.ReadCloser, error) // Media.Download(...).Download()
func Filter(r daterange.Range) string // `createTime >= "<From RFC3339Nano>" AND createTime < "<To RFC3339Nano>"`
// errors.go
var ErrUnauthenticated, ErrAccessNotConfigured, ErrSpaceNotFound error
type ScopeError struct{ Detail string }
type AmbiguousSpaceError struct{ Candidates []model.Space }
func Classify(err error) error // *googleapi.Error: 401→ErrUnauthenticated; 403 with reason ACCESS_TOKEN_SCOPE_INSUFFICIENT→*ScopeError; 403 SERVICE_DISABLED/accessNotConfigured→ErrAccessNotConfigured; 404→ErrSpaceNotFound; else unchanged
```

`ListMessages` params: `Filter(r)`, `OrderBy("createTime ASC")`, `PageSize(1000)`, `ShowDeleted(false)`, `PageToken(pageToken)`. Conversion: `IsThreadReply = msg.ThreadReply`; `EditedTime` set only when `lastUpdateTime` non-empty and ≠ `createTime`; `Text = msg.Text`; sender name = `msg.Sender.DisplayName`, else `members[msg.Sender.Name]`, else `msg.Sender.Name`; reactions from `EmojiReactionSummaries` (unicode emoji, or `:<customEmoji.uid>:` for custom); attachments: `source == "DRIVE_FILE"` → `SourceDrive` + DriveURL, else `SourceUploaded` + `AttachmentDataRef.ResourceName`; `QuotedMessageID` from `QuotedMessageMetadata.Name`.

- [ ] **Step 1: Write failing tests** (httptest server serving `testdata` JSON; assert request query params)

```go
func TestFilterString(t *testing.T)            // exact string for From=2026-09-01T00:00:00Z, To=2026-10-01T00:00:00Z
func TestListMessagesPagesAndParams(t *testing.T) // 2 pages; fn called twice with nextPageToken "p2" then ""; request had filter, orderBy, pageSize=1000
func TestListMessagesResumesFromToken(t *testing.T) // pageToken "p2" → first request carries pageToken=p2
func TestConvertMessage(t *testing.T)          // thread reply, edited, 2 reactions, 1 uploaded + 1 drive attachment, quoted msg
func TestSenderNameFallback(t *testing.T)      // empty sender displayName → members map → raw ID
func TestResolveSpace(t *testing.T)            // by name; by unique display name; zero → ErrSpaceNotFound; two → *AmbiguousSpaceError with 2 candidates
func TestClassify(t *testing.T)                // table over googleapi.Error codes/reasons
func TestDownloadMediaRequest(t *testing.T)    // GET /v1/media/<resource>?alt=media
```

- [ ] **Step 2: Run** `go test ./internal/chat/` → FAIL.
- [ ] **Step 3: Implement** using `chat.NewService(ctx, option.WithHTTPClient(hc), option.WithEndpoint(endpoint))`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: Chat API client wrapper and model conversion"`

---

### Task 8: JSONL read/write

**Files:**
- Create: `internal/render/jsonl.go`
- Test: `internal/render/jsonl_test.go`, `internal/render/testdata/messages.golden.jsonl`

**Interfaces:**
- Consumes: `model.Message`.
- Produces:
  - `func WriteJSONL(w io.Writer, m model.Message) error` — one line, record schema exactly as spec §5 (keys: `id, thread_id, is_thread_reply, time, edited_time, sender_id, sender_name, text, reactions, attachments[{name, content_type, source, path, sha256, drive_url}], quoted_message_id`); `time` RFC 3339 UTC; empty optional values → `null`; `reactions`/`attachments` → `[]` when empty. `SetEscapeHTML(false)`.
  - `func ReadJSONL(r io.Reader) ([]model.Message, error)` — inverse; line number in errors. Note: `attachments[].resource_name` is **not** emitted.

- [ ] **Step 1: Failing tests** — `TestWriteJSONLGolden` (fixed messages → golden file; `-update` flag regenerates), `TestJSONLRoundTrip` (write→read equals input minus `ResourceName`), `TestReadJSONLBadLine` (error mentions `line 2`).
- [ ] **Step 2: Run** `go test ./internal/render/ -run JSONL` → FAIL.
- [ ] **Step 3: Implement** via a private `record` struct with json tags.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: JSONL message format"`

---

### Task 9: Markdown renderer

**Files:**
- Create: `internal/render/markdown.go`, `internal/render/escape.go`
- Test: `internal/render/markdown_test.go`, `internal/render/escape_test.go`, `internal/render/testdata/transcript.golden.md`

**Interfaces:**
- Consumes: `model.*`, `daterange.Range`.
- Produces:
  - `type Header struct { Space model.Space; Title string; Range daterange.Range; GeneratedAt time.Time }` — `Title` precomputed by caller (Task 11) for unnamed spaces.
  - `func RenderMarkdown(w io.Writer, h Header, msgs []model.Message) error` — times in `h.Range.Loc`.
  - `func escapeText(s string) string`, `func safeLink(u string) (string, bool)` (unexported, tested in-package).

Layout: `# <Title>`; meta lines (type, range `From – To (<Loc>)`, generated-at, message count); messages grouped into threads keyed by `ThreadID` (messages with empty ThreadID are their own thread); thread order = time of first message present; `## YYYY-MM-DD` heading when a thread's first message starts a new day; first message: `**Sender** · HH:MM` (+ ` (edited)`), then text; replies: same block prefixed with `> `; if the thread's first present message has `IsThreadReply`, prefix its header with `↳ reply in thread · `; reactions line `👍 3 · 🎉 1`; attachments as `- [name](attachments/…)` or `- [name](drive URL)`; quoted message as `> _quoting <id>_`.

`escapeText`: `&`→`&amp;`, `<`→`&lt;`, `>`→`&gt;`, and backslash-escape `[` `]` so message text can never form a link. `safeLink`: allow `https`, `http`, or relative paths beginning `attachments/` with no `..`; anything else → (plain text, false).

- [ ] **Step 1: Failing tests**

```go
func TestEscapeText(t *testing.T)   // "<script>alert(1)</script>" → contains no "<script"; "[x](javascript:alert(1))" → no "](" sequence
func TestSafeLink(t *testing.T)     // javascript:, data:, file:, "attachments/../x" → false; https://…, attachments/a.png → true
func TestRenderGolden(t *testing.T) // threads, replies, edits, reactions, both attachment kinds, two days, tz=Asia/Kolkata
func TestOrphanReply(t *testing.T)  // Review Focus 2: only reply present → output contains "↳ reply in thread" and its text
func TestMaliciousAttachmentName(t *testing.T) // attachment Name "](javascript:x)" rendered escaped
```

- [ ] **Step 2: Run** `go test ./internal/render/` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: Markdown transcript renderer with safe escaping"`

---

### Task 10: Attachment downloader

**Files:**
- Create: `internal/attach/download.go`
- Test: `internal/attach/download_test.go`

**Interfaces:**
- Consumes: `fsutil.SanitizeName`, `fsutil.CreateNew`, `fsutil.RemoveRegular`, `model.Attachment`, `model.Message`.
- Produces:

```go
type FetchFunc func(ctx context.Context, resourceName string) (io.ReadCloser, error) // chat.Client.DownloadMedia fits
type Downloader struct { Fetch FetchFunc; Root string /* export dir */; MaxSize int64 }
var ErrTooLarge = errors.New("attachment exceeds size limit")
func (d *Downloader) DownloadAll(ctx context.Context, m model.Message) (model.Message, []error)
```

Per uploaded attachment: rel path `attachments/<ShortID>_<idx>_<SanitizeName(Name)>` (idx = position in message, guarantees uniqueness); if a regular file already exists there → `RemoveRegular` first (resume case), symlink → error; stream through `io.LimitReader(body, MaxSize+1)` while hashing SHA-256; if bytes > MaxSize → close, `RemoveRegular`, `ErrTooLarge`; on success set `Path`, `SHA256`, `Size`. Drive attachments untouched. Errors are per-attachment; others continue.

- [ ] **Step 1: Failing tests**

```go
func TestDownloadWritesFileAndHash(t *testing.T) // path, sha256 hex, size, file mode 0600
func TestDownloadTooLarge(t *testing.T)          // MaxSize 10, body 11 bytes → ErrTooLarge, file absent
func TestDownloadCollisionsAndEmptyNames(t *testing.T) // Review Focus 3: names "image.png","image.png","","..." → 4 distinct files, all exist
func TestDownloadTraversalName(t *testing.T)     // Name "../../x" → file inside Root/attachments
func TestDownloadReplacesExistingRegular(t *testing.T) // pre-existing file replaced
func TestDownloadRefusesSymlink(t *testing.T)    // pre-existing symlink at target → error, link target untouched
func TestDriveAttachmentSkipped(t *testing.T)    // Fetch never called
func TestPartialFailureContinues(t *testing.T)   // first fetch errors, second succeeds → 1 error, second has Path
```

- [ ] **Step 2: Run** `go test ./internal/attach/` → FAIL.
- [ ] **Step 3: Implement.** `attachments/` created via `fsutil.MkdirPrivate`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: bounded, sandboxed attachment downloader"`

---

### Task 11: Export orchestrator (output dir, resume, manifest)

**Files:**
- Create: `internal/export/export.go`, `internal/export/state.go`, `internal/export/manifest.go`
- Test: `internal/export/export_test.go`

**Interfaces:**
- Consumes: Tasks 1–3, 8–10; `chat.Client` methods via interface.
- Produces:

```go
type Source interface {
	ListMembers(ctx context.Context, space string) (map[string]string, error)
	ListMessages(ctx context.Context, space string, r daterange.Range, pageToken string, members map[string]string,
		fn func([]model.Message, string) error) error
	DownloadMedia(ctx context.Context, resourceName string) (io.ReadCloser, error)
}
type Options struct {
	Space model.Space; Range daterange.Range
	Markdown, JSONL bool; Attachments bool; MaxAttachmentSize int64
	OutRoot string; Resume, Force bool
	Scopes []string; Version string; Now func() time.Time
}
type ItemError struct { MessageID string `json:"message_id"`; Attachment string `json:"attachment,omitempty"`; Error string `json:"error"` }
type Result struct { Dir string; Messages, Attachments int; Errors []ItemError }
func (r Result) Partial() bool
var ErrOutputExists, ErrStateMismatch, ErrNoState error
func Run(ctx context.Context, src Source, opt Options, log *slog.Logger) (Result, error)
func Slug(s model.Space) string // SanitizeName(DisplayName) lowercased, spaces→"-"; empty → "dm-<ID>" / "group-<ID>" / "space-<ID>"
func Title(s model.Space, members map[string]string) string // DisplayName, else "DM with A, B" (sorted names, max 5)
```

Flow: dir = `OutRoot/Slug/Range.DirName()`; `fsutil.Contained(OutRoot, dir)`. Existing non-empty dir: `Resume` → load `.state.json` (missing → `ErrNoState`; space/range differ → `ErrStateMismatch`); `Force` → `os.RemoveAll(dir)` then recreate; neither → `ErrOutputExists`. State (`state.go`): `{space, from, to, page_token, jsonl_bytes, messages, attachments, errors}` written via `fsutil.WriteFileAtomic`. On resume, truncate `messages.jsonl` to `jsonl_bytes` before appending. Per page: `DownloadAll` (if enabled) → `WriteJSONL` each → `f.Sync()` → save state with new offset and `nextPageToken`. After last page: if `Markdown`, `ReadJSONL` + `RenderMarkdown` → `transcript.md` via `WriteFileAtomic`; if not `JSONL`, `RemoveRegular("messages.jsonl")`; write `manifest.json` (`tool_version, generated_at, space{name,display_name,type}, range{from,to,tz}, scopes, messages, attachments[{path,sha256,size}], errors`); `RemoveRegular(".state.json")`. Logs: counts and page numbers only.

- [ ] **Step 1: Failing tests** (fake `Source` in test file)

```go
func TestRunHappyPath(t *testing.T)        // 2 pages, files exist with 0600, dirs 0700, manifest counts match, no .state.json
func TestRunRefusesExisting(t *testing.T)  // ErrOutputExists
func TestRunForceReplaces(t *testing.T)
func TestRunResume(t *testing.T)           // Review Focus 4: fake fails after page 1 is written + partial page-2 bytes appended + stray attachment file; Resume → no duplicate IDs in JSONL, attachment replaced, Result.Messages == total
func TestRunResumeMismatch(t *testing.T)   // different range → ErrStateMismatch
func TestRunPartialAttachmentFailure(t *testing.T) // Result.Partial()==true, manifest.errors has 1 entry
func TestRunJSONLOnlyAndMDOnly(t *testing.T)
func TestSlugAndTitleForDM(t *testing.T)   // Review Focus 1: DM with empty DisplayName → "dm-AAAA", Title "DM with Alice, Bob"
func TestLogsContainNoMessageText(t *testing.T) // capture slog output; assert fake message text absent
```

- [ ] **Step 2: Run** `go test ./internal/export/` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/export/` → PASS.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: export orchestration with resume and manifest"`

---

### Task 12: CLI, config, logging, error messages

**Files:**
- Create: `internal/config/config.go`, `internal/cli/root.go`, `internal/cli/auth.go`, `internal/cli/spaces.go`, `internal/cli/export.go`, `internal/cli/logging.go`, `internal/cli/errors.go`, `internal/cli/browser.go`; modify `cmd/gchat-export/main.go`
- Test: `internal/config/config_test.go`, `internal/cli/logging_test.go`, `internal/cli/errors_test.go`, `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `config.Config{ClientSecret, OutDir, TZ string; RPS float64; InsecureFileStore bool}`; `func config.Load(path string, getenv func(string) string) (Config, error)` — file is **JSON** at `os.UserConfigDir()/gchat-export/config.json` (no TOML dep); env `GCHAT_EXPORT_CLIENT_SECRET`, `GCHAT_EXPORT_OUT`, `GCHAT_EXPORT_TZ`, `GCHAT_EXPORT_RPS`; flags applied last by cli (only if `Changed`).
  - `func cli.Execute(ctx context.Context, version string, stdout, stderr io.Writer, args []string) int` — returns exit code.
  - `func NewRedactingHandler(h slog.Handler) slog.Handler` — scrubs `ya29\.[\w.-]+`, `1//[\w.-]+`, `Bearer\s+\S+`, `"(access|refresh|id)_token"\s*:\s*"[^"]+"` from messages and string attrs.
  - `func userMessage(err error) string` — maps: `auth.ErrNoCredential`/`chat.ErrUnauthenticated` → "Not logged in or token revoked. Run: gchat-export auth login"; `*chat.ScopeError` → "Missing permission (<detail>). Run: gchat-export auth login"; `chat.ErrAccessNotConfigured` → "Chat API not enabled or app not allowed by your Workspace admin. See docs/setup.md#enable-the-api"; `chat.ErrSpaceNotFound` → "Space not found. Try: gchat-export spaces --filter <text>"; `*chat.AmbiguousSpaceError` → lists candidates; `export.ErrOutputExists` → "Output exists. Use --resume or --force."; default → `err.Error()` passed through the redactor.
  - `openBrowser(url string) error` — `exec.Command("open", url)` / `xdg-open` / `rundll32 url.dll,FileProtocolHandler`; never via a shell.

Commands per spec §2. `auth login`: warns (stderr) if `fsutil.CheckPrivate(clientSecret)` fails; `Login` (timeout 2 min) → `FetchTokenInfo` → store `Credential`. `auth status`: email + scopes; exit 1 if none. `auth logout`: `Revoke` (warn but continue on network failure) → `Delete`. HTTP client stack: `oauth2.NewClient(ctx, PersistingTokenSource)` whose base transport is `ratelimit.NewTransport(http.DefaultTransport, cfg.RPS)`. `export`: `--max-attachment-size` accepts `100MB`/`5MiB`/bytes; prints result dir and counts; exit 2 if `Partial()`. `--verbose` sets slog level Debug; default Warn. `spaces --json` prints `[]model.Space` JSON.

- [ ] **Step 1: Failing tests**

```go
func TestConfigPrecedence(t *testing.T)   // file < env (flags tested in cli_test)
func TestConfigRejectsUnknownKeys(t *testing.T) // json.Decoder.DisallowUnknownFields
func TestRedactingHandler(t *testing.T)   // each pattern scrubbed in msg and attrs; "[REDACTED]" present
func TestUserMessage(t *testing.T)        // table over all mapped errors
func TestExportFlagValidation(t *testing.T) // missing --space / --since → exit 1, stderr mentions flag; --format xml → exit 1
func TestNoTokenFlagsExist(t *testing.T)  // no flag name contains "token" or "secret" except --client-secret (a path)
func TestParseSize(t *testing.T)          // "100MB"=100_000_000, "5MiB"=5_242_880, "123"=123, "-1"/"x" error
```

- [ ] **Step 2: Run** `go test ./internal/config/ ./internal/cli/` → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./... && go vet ./...` → PASS; `go build ./cmd/gchat-export && ./gchat-export --help` lists `auth`, `spaces`, `export`.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: CLI commands, config, redacted logging"`

---

### Task 13: Repo hygiene, CI, releases, docs

**Files:**
- Create: `.github/workflows/ci.yml`, `.github/workflows/vulncheck.yml`, `.github/workflows/release.yml`, `.github/workflows/scorecard.yml`, `.github/dependabot.yml`, `.goreleaser.yaml`, `.golangci.yml`, `SECURITY.md`, `README.md`, `docs/setup.md`, `LICENSE` (Apache-2.0 unless the human picks otherwise — ask)
- Modify: `.gitignore` (add `/gchat-export` binary, `credential.json`)

Requirements:
- **ci.yml:** on PR/push to main; matrix `ubuntu-latest, macos-latest, windows-latest`; `permissions: contents: read`; steps: `go vet`, `go test -race ./...`, `staticcheck`, `golangci-lint`, `gosec ./...`, `govulncheck ./...`. Every `uses:` pinned to a full commit SHA with a `# vX.Y.Z` comment.
- **vulncheck.yml:** nightly cron `govulncheck`.
- **release.yml:** on tag `v*`; `permissions: contents: write, id-token: write, attestations: write`; goreleaser with `-trimpath`, `CGO_ENABLED=0`, `-ldflags "-s -w -X main.version={{.Version}}"`, `mod_timestamp: "{{ .CommitTimestamp }}"`; targets darwin/linux/windows × amd64/arm64; checksums SHA-256; `cosign sign-blob` keyless on checksums; SBOM via syft (CycloneDX); `actions/attest-build-provenance` for SLSA provenance.
- **dependabot.yml:** `gomod` and `github-actions`, weekly.
- **SECURITY.md:** supported versions, report via GitHub private vulnerability reporting, scope summary from spec §6 threat model.
- **docs/setup.md** (anchors referenced by Task 12: `#enable-the-api`): create GCP project → enable Google Chat API → OAuth consent screen **Internal** → add the 5 scopes → create **Desktop app** client → download JSON, `chmod 600` → `gchat-export auth login --client-secret <path>` → what a Workspace admin may need to allow (API Controls → trust the app).
- **README.md:** what it does, install (verify checksum + `cosign verify-blob` example), quickstart, flag reference, output layout, security notes and out-of-scope list from spec §6.

- [ ] **Step 1: Write files.**
- [ ] **Step 2: Verify** `goreleaser check` and `goreleaser release --snapshot --clean` succeed locally (install via `brew install goreleaser` — ask first); `actionlint` if available.
- [ ] **Step 3: Stage & hand off** — `git commit -m "chore: CI, signed reproducible releases, security docs"`

---

### Task 14: Manual end-to-end test (developer account)

**Files:**
- Create: `e2e/e2e_test.go` (`//go:build e2e`)

Reads `GCHAT_E2E_SPACE` and `GCHAT_E2E_SINCE` env vars (not secrets; token comes from the keychain after a manual `auth login`). Runs `cli.Execute` for `spaces` and a 7-day `export --attachments` into `t.TempDir()`; asserts exit 0/2, `manifest.json` parses, message count > 0, every manifest attachment SHA-256 matches its file, and `transcript.md` contains no `<script`.

- [ ] **Step 1: Write test.** Confirm `go test ./...` (no tag) does not compile it.
- [ ] **Step 2: Human runs** `go test -tags e2e ./e2e/ -v` with their account → PASS.
- [ ] **Step 3: Stage & hand off** — `git commit -m "test: opt-in end-to-end test against real Chat API"`
