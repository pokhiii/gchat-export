# Security Policy

## No warranty

gchat-export is provided **"AS IS", without warranty of any kind**, and you
use it **at your own risk**. See sections 7 and 8 of the [LICENSE](LICENSE)
(Apache-2.0). Nothing in this document is a promise, guarantee or service
commitment. It describes how the tool is designed and how to report problems.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub:
**Security → Report a vulnerability** on this repository (private
vulnerability reporting).

This is a volunteer project. Reports are handled on a best-effort basis, with
no guaranteed response time and no guarantee that an issue will be fixed or
that fixes will be backported. Only the latest release is ever considered.

Please include the version (`gchat-export --version`), your OS, and steps to
reproduce. Never include real tokens, client secrets, or exported messages in
a report.

## Your responsibilities

By using gchat-export you are responsible for:

- Making sure you are allowed to export the conversations you export, under
  your organization's policies, applicable law, and Google's terms of
  service.
- Your Google Cloud project and OAuth client, and any access you or your
  Workspace administrator grant to it.
- The security, storage, sharing and deletion of exported files and
  downloaded attachments.
- The security of the machine and OS account you run it on.

## Design notes

These notes describe what the code is **designed** to do, to help you judge
whether it fits your needs. They are not guarantees, and the software may
contain bugs.

### Sensitive data the tool handles

- An OAuth refresh token, which grants ongoing read access to your Chat
  messages.
- Exported conversation data.

### Risks the design tries to reduce

- Other local users and processes reading tokens or exports.
- Malicious message content or attachments from other chat members.
- Accidental leakage through logs, shell history or git.
- Compromised dependencies or CI.

### Measures in the code

- **Login:** the OAuth installed-app flow uses PKCE (S256) and a random
  `state`. The callback listener binds only to `127.0.0.1`, accepts a single
  callback, and times out after 2 minutes.
- **Read-only scopes:** the tool requests read-only Chat scopes (spaces,
  messages, memberships) plus `openid` and `email`, and no write or Drive
  scopes.
- **Token storage:** tokens are stored in the OS keychain by default. The
  file fallback is opt-in, `0600`, and refused if its permissions are looser.
  Tokens are not accepted from flags or environment variables. `auth logout`
  asks Google to revoke the token.
- **Logs:** logs and errors are designed to exclude tokens and message text;
  token-shaped strings are redacted.
- **Output files:** written `0600` in `0700` directories, atomically, and not
  through symlinks.
- **Attachments:**
  - Names are sanitized and kept inside the output directory.
  - Downloads are size-capped and SHA-256 hashed.
  - The tool never opens or executes them.
- **Markdown output:** HTML and link syntax in message text is escaped, and
  only `http(s)` links and relative attachment paths are emitted.
- **Terminal output:** control characters are stripped from untrusted names
  before printing.
- **Network:** no telemetry. The tool contacts Google OAuth and Chat API
  endpoints only.
- **Supply chain:**
  - Few dependencies.
  - `govulncheck`, `gosec`, `staticcheck` and `golangci-lint` run in CI.
  - GitHub Actions are pinned by commit SHA, and Dependabot is enabled.
  - Release builds are reproducible, with signed checksums, an SBOM and build
    provenance.

### Not addressed by the design

- An attacker who already controls your OS account or machine.
- What happens to exported files after the export.
- Messages Google no longer returns (deleted or retention-expired).
