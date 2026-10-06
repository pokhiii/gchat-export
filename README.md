# gchat-export

Export Google Chat conversations from a chosen space, group chat or DM, over a
time range. You get:

- **`transcript.md`**: a readable transcript, grouped by thread, with
  timestamps, edits, reactions and attachment links.
- **`messages.jsonl`**: one JSON object per message, easy to feed to scripts
  or LLMs.
- **`attachments/`** (optional): the files people uploaded, with SHA-256
  hashes.

It uses the official Google Chat API with your own OAuth client. Access is
read-only, the token is stored in your OS keychain, and there's no telemetry.

## Install

Download a release archive for your platform from the Releases page, then
verify it:

```bash
# 1. Verify the checksums file was signed by this repository's release workflow
cosign verify-blob checksums.txt \
  --signature checksums.txt.sig --certificate checksums.txt.pem \
  --certificate-identity-regexp 'https://github.com/OWNER/gchat-export/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 2. Verify your archive against it
shasum -a 256 --ignore-missing -c checksums.txt

# 3. Optional: verify SLSA build provenance
gh attestation verify gchat-export_*_darwin_arm64.tar.gz --repo OWNER/gchat-export
```

Or build from source (Go 1.27+):

```bash
go install github.com/OWNER/gchat-export/cmd/gchat-export@latest
```

## Quick start

One-time setup of your Google Cloud OAuth client (about 10 minutes): see
[docs/setup.md](docs/setup.md). Then:

```bash
gchat-export auth login --client-secret ~/.config/gchat-export/client.json
gchat-export spaces --filter team
gchat-export export --space "Team Room" --since 2026-09-01 --until 2026-10-01 --attachments
```

## Commands

| Command | What it does |
|---|---|
| `auth login` | Opens your browser to log in, then stores the token in the OS keychain |
| `auth status` | Shows the logged-in account and granted scopes |
| `auth logout` | Revokes the token at Google and deletes it locally |
| `spaces [--type space\|dm\|group] [--filter TEXT] [--json]` | Lists the spaces you can export |
| `export` | Exports one space (flags below) |

`export` flags:

| Flag | Default | Meaning |
|---|---|---|
| `--space` | (required) | `spaces/…` resource name, or exact display name |
| `--since` | (required) | Start, inclusive: `YYYY-MM-DD` or RFC 3339 |
| `--until` | now | End, exclusive |
| `--tz` | local | Time zone for dates and display, e.g. `Asia/Kolkata` |
| `--format` | `md,jsonl` | `md`, `jsonl`, or both |
| `--attachments` | off | Download uploaded files (Drive files are linked, not downloaded) |
| `--max-attachment-size` | `100MB` | Skip larger attachments (`5MiB`, `1GB`, or bytes) |
| `--out` | `./export` | Output root |
| `--resume` | off | Continue an interrupted export |
| `--force` | off | Replace an existing export of the same space and range |
| `--rps` | `10` | Maximum API requests per second |

Global flags: `--client-secret PATH`, `--config PATH`,
`--insecure-file-store`, `--verbose`.

**Exit codes:**

- `0`: success.
- `1`: error.
- `2`: the export finished, but some attachments failed. Details are in
  `manifest.json`.

### Configuration

The optional config file lives at `<user config dir>/gchat-export/config.json`.
On macOS that's `~/Library/Application Support/gchat-export/config.json`.

```json
{
  "client_secret": "/Users/me/.config/gchat-export/client.json",
  "out_dir": "/Users/me/chat-exports",
  "tz": "Asia/Kolkata",
  "rps": 10,
  "insecure_file_store": false
}
```

Precedence is flags, then environment variables (`GCHAT_EXPORT_CLIENT_SECRET`,
`GCHAT_EXPORT_OUT`, `GCHAT_EXPORT_TZ`, `GCHAT_EXPORT_RPS`), then the config
file. Tokens are never read from flags, environment variables or the config
file.

## Output layout

```
export/<space-name>/<from>_<to>/
  transcript.md
  messages.jsonl
  attachments/<message-id>_<n>_<file-name>
  manifest.json      # counts, range, tool version, scopes, attachment hashes, errors
```

DMs and unnamed group chats are named `dm-<id>` or `group-<id>`. All files are
created `0600` and directories `0700`.

## Security

[SECURITY.md](SECURITY.md) describes how the tool is designed to handle
tokens, exports and untrusted message content, and how to report
vulnerabilities privately. Those notes are design intentions, not
guarantees.

## Disclaimer

- This software is provided **"AS IS", without warranty of any kind**, and
  you use it **at your own risk** (see [LICENSE](LICENSE), sections 7 and 8).
- You are responsible for making sure you are allowed to export the
  conversations you export, under your organization's policies, applicable
  law and Google's terms. You are also responsible for the security,
  sharing and deletion of exported files.
- gchat-export is an independent project. It is not affiliated with,
  endorsed by or sponsored by Google. "Google" and "Google Chat" are
  trademarks of Google LLC.

## License

Apache-2.0. See [LICENSE](LICENSE).
