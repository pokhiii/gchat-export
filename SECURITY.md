# Security Policy

## Supported versions

Security fixes are released for the latest minor version only.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub:
**Security → Report a vulnerability** on this repository (private
vulnerability reporting). Expect an acknowledgement within 7 days.

Please include the version (`gchat-export --version`), your OS, and steps to
reproduce. Never include real tokens, client secrets, or exported messages in
a report.

## Threat model (summary)

**What we protect**

- The OAuth refresh token, which grants ongoing read access to your Chat
  messages.
- Exported conversation data.

**Who we defend against**

- Other local users and processes.
- Malicious message content and attachments sent by other chat members.
- Accidental leakage through logs, shell history or git.
- Compromised dependencies or CI.

**Controls**

- **Login:** the OAuth installed-app flow uses PKCE (S256) and a random
  `state`. The callback listener binds only to `127.0.0.1`, accepts a single
  callback, and times out after 2 minutes.
- **Read-only scopes:** the Chat scopes are spaces, messages and memberships,
  all read-only, plus `openid` and `email`. The tool never requests write or
  Drive access.
- **Token storage:** tokens live in the OS keychain. The file fallback is
  opt-in, `0600`, and refused if its permissions are looser. Tokens are never
  accepted from flags or environment variables. `auth logout` revokes the
  token at Google.
- **No secrets in logs:** logs and errors never contain tokens or message
  text, and token-shaped strings are redacted.
- **Output files:** written `0600` in `0700` directories, atomically, and never
  through symlinks.
- **Attachments are untrusted:**
  - File names are sanitized and kept inside the output directory.
  - Downloads are size-capped and SHA-256 hashed.
  - Files are never opened or executed.
- **Markdown output:** HTML and link syntax in message text is neutralized.
  Only `http(s)` links and relative attachment paths are emitted.
- **Terminal output:** control characters are stripped from untrusted names
  before printing.
- **Network:** no telemetry. The tool contacts only Google OAuth and Chat API
  endpoints.
- **Supply chain:**
  - Minimal dependencies.
  - `govulncheck`, `gosec`, `staticcheck` and `golangci-lint` in CI.
  - GitHub Actions pinned by commit SHA; Dependabot.
  - Reproducible builds with signed checksums (Sigstore cosign), an SBOM, and
    SLSA build provenance.
  - OpenSSF Scorecard.

**Out of scope**

- An attacker who already controls your OS account.
- What happens to exported files after the export.
- Messages Google no longer returns (deleted or retention-expired).
