# Setup

gchat-export uses the official Google Chat API with **your own** OAuth client.
Nothing secret ships with the tool, and Google never sees a third-party app:
the client lives in a Google Cloud project you control.

You need a **Google Workspace** account. The Chat API does not support
personal `@gmail.com` accounts for this kind of access.

Setup takes about 10 minutes and is done once.

## 1. Create a Google Cloud project

1. Open <https://console.cloud.google.com/projectcreate>.
2. Name it, for example `gchat-export`, and create it inside your Workspace
   organization.

## Enable the API

1. Open <https://console.cloud.google.com/apis/library/chat.googleapis.com>
   with your new project selected.
2. Click **Enable**.

The Chat API also needs a minimal "Chat app" configuration even for user
access. Open **APIs & Services → Google Chat API → Configuration**, fill in an
app name, avatar URL and description, untick **Interactive features**, and
save. The app does not need to be published or visible to anyone.

## 2. Configure the OAuth consent screen

1. Open **Google Auth Platform → Branding** (or **APIs & Services → OAuth
   consent screen**).
2. Set **Audience** to **Internal**. Internal apps are limited to your
   Workspace and need no Google verification.
3. Under **Data access**, add exactly these scopes:
   - `https://www.googleapis.com/auth/chat.spaces.readonly`
   - `https://www.googleapis.com/auth/chat.messages.readonly`
   - `https://www.googleapis.com/auth/chat.memberships.readonly`
   - `openid`
   - `email`

All of them are read-only. gchat-export never asks for write access or Drive
access.

## 3. Create a Desktop OAuth client

1. Open **Google Auth Platform → Clients → Create client**.
2. Application type: **Desktop app**. Web clients are rejected by the tool.
3. Download the JSON file and keep it private:

   ```bash
   mkdir -p ~/.config/gchat-export
   mv ~/Downloads/client_secret_*.json ~/.config/gchat-export/client.json
   chmod 600 ~/.config/gchat-export/client.json
   ```

For Desktop clients Google treats the "client secret" as non-confidential, but
there is no reason to share it. Never commit it; the repository's
`.gitignore` already excludes `client_secret*.json`.

## 4. Log in

```bash
gchat-export auth login --client-secret ~/.config/gchat-export/client.json
```

Your browser opens Google's consent page. After you approve, the token is
stored in your OS keychain (macOS Keychain, Windows Credential Manager, or
Linux Secret Service). The path to the client file is remembered, so later
commands don't need `--client-secret`.

On a machine without a keychain, add `--insecure-file-store` to every command
(or set `"insecure_file_store": true` in the config file). The token is then
kept in a `0600` file in your user config directory.

## If your Workspace admin restricts apps

If login or the first API call fails with "app not allowed" or
"access blocked", a Workspace admin needs to trust your OAuth client:

1. Admin console → **Security → Access and data control → API controls**.
2. **Manage Third-Party App Access → Configure new app**, search for your
   OAuth client ID, and set it to **Trusted** (or **Limited**, with the Chat
   scopes allowed).

## Removing access

```bash
gchat-export auth logout
```

This revokes the token at Google and deletes it locally. You can also review
access at <https://myaccount.google.com/permissions>.
