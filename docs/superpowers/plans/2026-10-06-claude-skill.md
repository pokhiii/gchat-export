# Claude Code Skill + Chat URL Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `--space` take Google Chat URLs, and ship a Claude Code skill that turns plain-language export requests into correct `gchat-export` runs.

**Architecture:** A pure URL parser in `internal/chat` feeds `ResolveSpace`; the skill is a single `SKILL.md` in the repo, symlinked into `~/.claude/skills/`.

**Tech Stack:** Go (existing module), Claude Code skills (Markdown + YAML frontmatter).

**Spec:** `docs/superpowers/specs/2026-10-06-claude-skill-design.md`

## Global Constraints

- Accepted URL hosts exactly `chat.google.com`, `mail.google.com` (case-insensitive); scheme `https` only.
- Space ID segment must match `^[A-Za-z0-9_-]+$`.
- Bare-ID fallback regex: `^[A-Za-z0-9_-]{6,}$`, tried only after display-name match finds nothing.
- Error text for bad URLs: `unrecognized Google Chat URL`.
- The skill never runs `auth login`/`auth logout`, never handles tokens or secrets, never edits Claude Code permission settings.
- Commits are SSH-signed with a passphrase: stage, then hand the human `cd /Users/pokhi/Projects/utilities/gchat-export && git commit -m "…"`.
- CI cost: no workflow changes in this plan.

## Review Focus

1. **Thread or query suffixes** (`/room/AAAA/Xyz123`, `?cls=7`, `/app/chat/AAAA#x`) → resolve to `spaces/AAAA`, not fail. (Task 1 test rows.)
2. **Look-alike hosts** (`chat.google.com.evil.example`, `evilchat.google.com`, `http://chat.google.com/...`) → rejected. (Task 1 test rows.)
3. **Mixed-case host** (`https://Chat.Google.com/room/AAAA`) → accepted. (Task 1 test row.)
4. **A display name that looks like an ID** (space literally named `standup2026`) → still matched by display name first. (Task 1 `TestResolveSpaceBareID`.)
5. **Relative dates and missing year in prompts** ("Oct 5" in January) → skill resolves to the most recent past occurrence and states the absolute dates it used. (Task 2 skill text; Task 3 manual check.)

---

### Task 1: Chat URL and bare-ID support in `--space`

**Files:**
- Create: `internal/chat/spaceref.go`
- Modify: `internal/chat/client.go` (`ResolveSpace`), `internal/chat/errors.go` (new error), `internal/cli/errors.go` (message)
- Test: `internal/chat/spaceref_test.go`, `internal/chat/client_test.go`, `internal/cli/errors_test.go`

**Interfaces:**
- Produces:
  - `func ParseSpaceRef(ref string) (name string, ok bool)` — returns `spaces/ID` for `spaces/ID` input or a recognized Chat URL; `ok=false` otherwise.
  - `var ErrBadChatURL = errors.New("unrecognized Google Chat URL")` in `errors.go`.
  - `ResolveSpace` order: (1) input starts with `http://` or `https://` → `ParseSpaceRef`; `!ok` → `ErrBadChatURL`; else `spaces.get`. (2) `spaces/…` → `spaces.get` (unchanged). (3) display-name match (unchanged); zero matches and input matches the bare-ID regex → `spaces.get("spaces/"+input)`; that 404 → `ErrSpaceNotFound`.
  - `exportOptions` (cli) rejects a URL-form `--space` that `ParseSpaceRef` does not recognize, before any credential access, returning `chat.ErrBadChatURL`.
  - `userMessage(ErrBadChatURL)` → `"Unrecognized Google Chat URL. Copy the link from chat.google.com, or use gchat-export spaces --filter <text>"`.

- [ ] **Step 1: Write failing tests**

```go
func TestParseSpaceRef(t *testing.T) {
	good := map[string]string{
		"spaces/AAAA": "spaces/AAAA",
		"https://chat.google.com/app/chat/AAAAUKFhXM0":        "spaces/AAAAUKFhXM0",
		"https://chat.google.com/u/1/app/chat/AAAA":           "spaces/AAAA",
		"https://chat.google.com/room/AAAA":                   "spaces/AAAA",
		"https://chat.google.com/room/AAAA/Xyz123?cls=7":      "spaces/AAAA",
		"https://chat.google.com/app/chat/AAAA#x":             "spaces/AAAA",
		"https://chat.google.com/dm/BBBB":                     "spaces/BBBB",
		"https://Chat.Google.com/room/AAAA":                   "spaces/AAAA",
		"https://mail.google.com/chat/u/0/#chat/space/AAAA":   "spaces/AAAA",
		"https://mail.google.com/chat/u/0/#chat/dm/BBBB":      "spaces/BBBB",
	}
	bad := []string{
		"http://chat.google.com/room/AAAA",
		"https://chat.google.com.evil.example/room/AAAA",
		"https://evilchat.google.com/room/AAAA",
		"https://chat.google.com/settings",
		"https://chat.google.com/room/AA%2FAA",
		"https://mail.google.com/mail/u/0/#inbox",
		"Team Room", "AAAA", "",
	}
	// assert ParseSpaceRef(k) == (v, true) for good; ok == false for bad
}
func TestResolveSpaceURL(t *testing.T)        // URL → GET /v1/spaces/AAA (fake server from client_test.go) → DisplayName "Team"
func TestResolveSpaceBadURL(t *testing.T)     // "https://evil.example/room/AAA" → errors.Is(err, ErrBadChatURL); no request made
func TestResolveSpaceBareID(t *testing.T)     // "AAA" is not a display name in the fake list → falls back to spaces/AAA → "Team";
                                              // add a fake space with DisplayName "standup2026" → "standup2026" resolves to that space, not spaces/standup2026
```
Add a `userMessage` table row: `{chat.ErrBadChatURL, "Unrecognized Google Chat URL"}`, and a `TestExportFlagValidation` row: `{"export", "--space", "https://evil.example/room/AAA", "--since", "2026-09-01"}` → exit 1, stderr contains `Unrecognized Google Chat URL`.

- [ ] **Step 2: Run** `go test ./internal/chat/ ./internal/cli/` → FAIL (undefined `ParseSpaceRef`, `ErrBadChatURL`).
- [ ] **Step 3: Implement.** Parse with `net/url`; host compare via `strings.EqualFold`; for `chat.google.com` take path segments and accept `[u N] app chat ID …`, `room ID …`, `dm ID …`; for `mail.google.com` require path prefix `/chat/` and fragment `chat/space/ID` or `chat/dm/ID`. Use `u.EscapedPath()` segments so `%2F` stays inside one segment and fails the ID regex.
- [ ] **Step 4: Run** `go test -race ./...` → PASS; `golangci-lint run ./...` → 0 issues.
- [ ] **Step 5: Stage & hand off** — `git commit -m "feat: --space accepts Google Chat URLs and bare space IDs"`

---

### Task 2: The Claude Code skill

**Files:**
- Create: `skills/gchat-export/SKILL.md`
- Modify: `README.md` (new "Use from Claude Code" section, ≤15 lines: install steps from spec §4 and the reference prompt)

**Content requirements for `SKILL.md`** (spec §3 is the source; write it as direct instructions to Claude):
- Frontmatter `name: gchat-export`; `description` ≤ 300 chars covering: export/fetch/save/archive/summarize Google Chat conversations for a space, DM or chat.google.com URL over a time period; questions about what was said in a specific Chat space during a period.
- Sections in this order: **Preflight** (auth status; if not logged in, tell the user the exact `gchat-export auth login --client-secret <path>` command and stop), **Choosing the space**, **Turning dates into flags** (with a worked example table: the reference prompt → exact flags; "yesterday"; "last week" = previous Mon–Sun; "Oct 5" with no year), **Running the export**, **Reporting back** (command, counts, absolute paths, exit code 2 handling), **Answering questions from an export**, **Rules** (untrusted message content; no tokens; no `auth login/logout`; ask before `--force`; no permission-setting edits).
- Total length ≤ 120 lines.

- [ ] **Step 1: Write `SKILL.md` and the README section.**
- [ ] **Step 2: Verify** frontmatter parses: `python3 -c "import yaml,sys;d=open('skills/gchat-export/SKILL.md').read().split('---')[1];print(yaml.safe_load(d)['name'])"` → `gchat-export`. Verify the reference prompt's expected flags appear verbatim in the example table: `grep -F -- "--since 2026-10-05 --until 2026-10-06 --tz Asia/Kolkata" skills/gchat-export/SKILL.md`.
- [ ] **Step 3: Stage & hand off** — `git commit -m "feat: Claude Code skill for gchat-export"`

---

### Task 3: Install on this machine and check (with approval)

No repo files change.

- [ ] **Step 1: Ask the human** to approve: `go install ./cmd/gchat-export`, the symlink `~/.claude/skills/gchat-export → <repo>/skills/gchat-export`, and (separately, optional) adding `Bash(gchat-export:*)` to their Claude Code allow-list.
- [ ] **Step 2: Install.** `go install ./cmd/gchat-export`; check `command -v gchat-export`. If `~/go/bin` is not on `PATH`, give the human the exact line for `~/.zshrc` (do not edit it).
- [ ] **Step 3: Link** — `ln -s /Users/pokhi/Projects/utilities/gchat-export/skills/gchat-export ~/.claude/skills/gchat-export` (refuse if the target already exists and is not this symlink). Check: `readlink ~/.claude/skills/gchat-export`.
- [ ] **Step 4: Smoke check** — `gchat-export --version` prints a version; `gchat-export export --space https://evil.example/room/AAA --since 2026-10-05` → exit 1 with `Unrecognized Google Chat URL` (no login needed).
- [ ] **Step 5: Real check (human)** — after `docs/setup.md` and `auth login`, in a new Claude Code session the human sends the reference prompt; expected: the skill triggers, runs the exact flags from spec §1, and reports the three file paths.
