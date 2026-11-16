# telegram-cli specification

Status: approved design for implementation. Build order lives in [plan.md](plan.md).

## 1. Purpose and scope

`telegram-cli` is a command-line Telegram client for one personal user account. It talks
MTProto directly through [gotd/td](https://github.com/gotd/td) and uses
[cobra](https://github.com/spf13/cobra) for the command tree. It is meant for scripts and
for people who live in a terminal: every command prints readable text by default and a
stable JSON shape with `--json`.

What it does: log in, show the account, list chats, read and search messages, send text,
upload files and media, download media, export a chat to JSON or Markdown, and stream
new messages.

Fixed decisions:

- User accounts only (MTProto user login). No bot-token mode.
- One account. No profiles, no account switching.
- Installed with `go install github.com/nphuoctho/telegram-cli@latest`. No release
  binaries, packages, or installers.
- No TUI. Prompts are plain line reads.

Out of scope (not planned): bots, multiple accounts, secret chats, calls, editing or
deleting messages, reactions, stickers, polls, contacts management, global search across
all chats, Markdown/HTML formatting of sent text, albums, proxies, and a `--debug` log mode.

## 2. Getting an API ID and API hash

Telegram requires every client app to identify itself with an API ID and API hash.

1. Sign in at <https://my.telegram.org> with the phone number of your account.
2. Open **API development tools**.
3. Fill in the form (any app title and short name; platform "Desktop" or "Other").
4. Copy **App api_id** (a number) and **App api_hash** (32 hex characters).
5. Run `telegram-cli init` and paste both values, or export `TELEGRAM_APP_ID` and
   `TELEGRAM_APP_HASH`.

The API hash is a secret. Do not share it or commit it.

## 3. Files, formats, and precedence

### 3.1 Locations

| What | Path | Dir mode | File mode |
|------|------|----------|-----------|
| Config (non-secret) | `$XDG_CONFIG_HOME/telegram-cli/config.toml`, default `~/.config/telegram-cli/config.toml` | 0700 | 0600 |
| API credentials | `$XDG_DATA_HOME/telegram-cli/credentials.json`, default `~/.local/share/telegram-cli/credentials.json` | 0700 | 0600 |
| MTProto session | `<data dir>/session.json` | 0700 | 0600 |
| Peer access-hash cache | `<data dir>/peers.json` | 0700 | 0600 |

The same paths are used on every OS (we do not use `os.UserConfigDir`, which points to
`~/Library/Application Support` on macOS). `XDG_CONFIG_HOME` and `XDG_DATA_HOME` are honored
when set to an absolute path; otherwise `$HOME` is used.

Permissions:

- Directories are created with 0700 and files are written with 0600.
- Every write goes to a temp file in the same directory, then `os.Rename` (atomic).
- On start, if the data dir or any file in it is readable by group or others, the CLI
  tightens it to 0700/0600 and prints one warning line to stderr.

### 3.2 config.toml

Optional. Missing file means defaults. `init` writes it with defaults if it does not exist.
Unknown keys are an error (exit 2) so typos are visible.

```toml
# Directory where `download` saves files when --dir is not given. "~" is expanded.
download_dir = "."

# Longest FLOOD_WAIT the CLI will sleep through before giving up (Go duration).
max_flood_wait = "2m"
```

Secrets are never read from config.toml. If it contains `api_id`, `api_hash`, or `session`,
that is an unknown-key error.

### 3.3 credentials.json

```json
{"api_id": 1234567, "api_hash": "0123456789abcdef0123456789abcdef"}
```

Validation: `api_id` is a positive integer; `api_hash` matches `^[0-9a-f]{32}$`.

### 3.4 session.json and peers.json

- `session.json` is written and read by gotd through `session.FileStorage`. Its content is
  opaque to us. It holds the account's auth key: anyone with this file is logged in as you.
- `peers.json` maps peer keys to access hashes so numeric chat IDs can be resolved across
  runs: `{"users_123": 456, "channel_789": 1011}` (key = gotd `peers.Key` prefix + ID,
  value = access hash). Phone numbers and the contacts hash are kept in memory only and never
  written to disk.

### 3.5 Precedence

For every setting: command-line flag > environment variable > file > built-in default.

| Value | Flag | Env var | File |
|-------|------|---------|------|
| API ID | none | `TELEGRAM_APP_ID` | credentials.json `api_id` |
| API hash | none | `TELEGRAM_APP_HASH` | credentials.json `api_hash` |
| Download dir | `download --dir` | none | config.toml `download_dir` |
| Max flood wait | none | none | config.toml `max_flood_wait` |

Each env var overrides only its own field. If either API value is missing after merging,
network commands exit with code 3 and say `run "telegram-cli init" or set TELEGRAM_APP_ID
and TELEGRAM_APP_HASH`. There are deliberately no flags for the API hash, so it does not
end up in shell history or `ps` output.

## 4. Global behavior

### 4.1 Output

- Data goes to stdout. Prompts, progress, warnings, and errors go to stderr.
- `--json` (persistent flag on the root) switches stdout to JSON: one JSON document per
  command, except `watch`, which writes JSON Lines (one object per line).
- Times in readable output use the local time zone, `2006-01-02 15:04`. Times in JSON are
  RFC 3339 with offset.
- Sizes in readable output use binary units (`1.5 MiB`). JSON sizes are integer bytes.
- Lists of messages are always printed oldest first.
- `telegram-cli --version` prints the module version from `debug.ReadBuildInfo`
  (`(devel)` for local builds).

### 4.2 Errors

Readable mode prints `error: <message>` to stderr. With `--json`, stderr gets:

```json
{"error": {"code": "AUTH_REQUIRED", "message": "not logged in; run \"telegram-cli auth login\""}}
```

### 4.3 Exit codes

| Code | `error.code` | Meaning |
|------|--------------|---------|
| 0 | | Success |
| 1 | `ERROR` | Any other failure (network, Telegram RPC error, local file I/O) |
| 2 | `USAGE` | Bad arguments, flags, or config file |
| 3 | `NOT_CONFIGURED` | API ID or hash missing, or Telegram returned `API_ID_INVALID` |
| 4 | `AUTH_REQUIRED` | No session, or Telegram returned 401 |
| 5 | `PEER_NOT_FOUND` | Chat identifier could not be resolved or is not accessible |
| 6 | `FLOOD_WAIT` | Telegram asked to wait longer than `max_flood_wait` |
| 130 | `INTERRUPTED` | Ctrl-C (context canceled by SIGINT/SIGTERM) |

Commands return typed errors; only `cmd.Execute` maps them to exit codes and prints them.

## 5. Chat identifiers

Every `<chat>` argument accepts:

| Form | Example | Meaning |
|------|---------|---------|
| `me` | `me` | Saved Messages (your own chat); case-insensitive |
| Numeric ID | `123456789`, `-4012345`, `-1001234567890` | Bot-API style ID as printed by `chats list` |
| Username | `@durov`, `durov`, `https://t.me/durov` | Public user, bot, group, or channel |
| Phone | `+15551234567` | A user in your contacts |

Numeric IDs use the TDLib/Bot API convention (gotd `constant.TDLibPeerID`): positive for
users, `-id` for basic groups, `-100…` for channels and supergroups.

Resolution order:

1. `me` → `tg.InputPeerSelf`.
2. Matches `^-?[0-9]+$` → `peers.Manager.ResolveTDLibID`. The access hash comes from
   `peers.json`. If the peer is not found, the CLI walks the dialog list once (applying every
   page's users and chats to the manager, which saves their hashes), then retries. Still not
   found → exit 5 with `chat <id> not found; check "telegram-cli chats list"`.
3. Anything else → `peers.Manager.Resolve` (handles `@name`, bare names, t.me links,
   `tg://resolve` links, and phone numbers).

Plain digits are always an ID, never a phone number: a phone must start with `+`. The
check in step 2 runs before `peers.Manager.Resolve`, which would otherwise treat digit
strings as phone numbers.

These all map to exit 5: `*peers.PeerNotFoundError`, `*peers.PhoneNotFoundError`, and RPC
errors `USERNAME_NOT_OCCUPIED`, `USERNAME_INVALID`, `PEER_ID_INVALID`, `CHANNEL_INVALID`,
`CHANNEL_PRIVATE`.

## 6. Shared JSON objects

**User**

```json
{"id": 123456789, "first_name": "Alice", "last_name": "", "username": "alice",
 "phone": "15551234567", "bot": false, "premium": false}
```

`phone` is present only for `me`/`auth status` (your own account), never for other users.

**Chat**

```json
{"id": -1001234567890, "type": "supergroup", "title": "Go Developers",
 "username": "golang", "unread": 3}
```

`type` is one of `user`, `bot`, `group`, `supergroup`, `channel`. `title` is the full name
for users. `username` is `""` when absent. `unread` appears only in `chats list`.

**Message**

```json
{
  "id": 1234,
  "chat_id": -1001234567890,
  "date": "2025-11-16T14:02:05+07:00",
  "from": {"id": 123456789, "name": "Alice", "username": "alice"},
  "out": false,
  "text": "Hello there",
  "reply_to_id": 1200,
  "media": {"type": "photo", "file_name": "photo5012_2025-11-16_14-02-05.jpg",
            "mime_type": "image/jpeg", "size": 183244},
  "file": "go-developers_media/1234_photo5012_2025-11-16_14-02-05.jpg",
  "service": false
}
```

- `from` is `null` for channel posts without a signature.
- `reply_to_id`, `media`, and `file` are omitted when empty. `file` appears only in
  `export --media` output and is relative to the export file.
- `media.type` is `photo`, `video`, `audio`, `voice`, `document`, `sticker`, `gif`, or
  `other` (location, poll, contact, and so on; then `file_name`/`size` are omitted). Link previews (web pages) are not media: `media` is omitted for them.
- Service messages (joins, pins, title changes) have `"service": true` and `text` set to
  the action name, e.g. `"MessageActionChatAddUser"`.

Readable message format (used by `messages`, `watch`, and as a model for Markdown export):

```
#1234  2025-11-16 14:02  Alice (@alice)  ↩1200
  Hello there
  [photo] photo5012_2025-11-16_14-02-05.jpg (178.9 KiB)
```

## 7. Command reference

Global flags: `--json`, `--help`, `--version` (root only).

Commands that need the network open one MTProto connection, check authorization, run, and
close. They exit 3 if credentials are missing and 4 if not logged in.

### 7.1 `init`

```
telegram-cli init [--force]
```

Prompts for API ID and API hash, validates them (section 3.3), and writes
`credentials.json`. Creates `config.toml` with defaults if missing. No network.

- The hash prompt hides input when stdin is a terminal (`golang.org/x/term`). When stdin is
  not a terminal, the two values are read as two lines from stdin.
- If `credentials.json` exists, `init` refuses (exit 2) unless `--force` is given.
- Invalid input exits 2 without writing anything.

```
$ telegram-cli init
API ID: 1234567
API hash:
Saved credentials to /home/alice/.local/share/telegram-cli/credentials.json
Wrote default config to /home/alice/.config/telegram-cli/config.toml
```

JSON: `{"credentials": "<path>", "config": "<path>", "config_created": true}`

### 7.2 `auth login`

```
telegram-cli auth login [--phone +15551234567]
```

Runs the gotd user auth flow: phone (prompt if `--phone` is not given), login code
(prompt; the message says where Telegram sent it: app, SMS, call), then the 2FA password
(hidden prompt) if the account has one. Saves the session on success. If the session is
already authorized, prints the status and exits 0 without prompting.

- Sign-up is not supported. If the phone has no account: exit 1 with `no Telegram account
  for this number; sign up in an official app first`.
- Wrong code or password: exit 1 with Telegram's error (`PHONE_CODE_INVALID`,
  `PASSWORD_HASH_INVALID`). No retry loop.
- Non-terminal stdin: code and password are read as lines from stdin.

```
$ telegram-cli auth login
Phone (international format): +15551234567
Code sent to the Telegram app: 12345
2FA password:
Logged in as Alice (@alice, id 123456789)
```

JSON: `{"user": User}`

### 7.3 `auth logout`

```
telegram-cli auth logout
```

Calls `auth.logOut` on Telegram, then deletes `session.json` and `peers.json`. Keeps
`credentials.json` and `config.toml`. Local files are deleted even when the server call
fails; then it exits 1 with `local session removed, but Telegram logout failed: <err>;
end the session in Settings > Devices`. With no session file it prints `Not logged in`
and exits 0.

JSON: `{"logged_out": true}`

### 7.4 `auth status`

```
telegram-cli auth status
```

Shows where credentials come from and whether the session is valid. Exits 4 when not
logged in and 3 when credentials are missing, so scripts can test it. It still prints its
report (readable or JSON) to stdout in those cases.

```
$ telegram-cli auth status
Credentials: file (/home/alice/.local/share/telegram-cli/credentials.json)
Logged in as Alice (@alice, id 123456789)
```

JSON: `{"credentials": "env|file|mixed|none", "authorized": true, "user": User}` (`user` is
`null` when not authorized).

### 7.5 `me`

```
telegram-cli me
```

```
ID:        123456789
Name:      Alice Liddell
Username:  @alice
Phone:     +15551234567
Premium:   no
```

JSON: `User`.

### 7.6 `chats list`

```
telegram-cli chats list [--limit 50]
```

Lists dialogs in Telegram's order (pinned first, then by last activity). `--limit 0`
lists all. Also refreshes `peers.json` with every chat it sees.

```
ID              TYPE        UNREAD  NAME
777000          user        0       Telegram
-1001234567890  supergroup  3       Go Developers (@golang)
123456789       user        0       Saved Messages
```

JSON: `[Chat, ...]`

### 7.7 `messages read`

```
telegram-cli messages read <chat> [--limit 20] [--before <msg-id>]
```

Prints the newest `--limit` messages (max 1000) older than `--before` (default: newest),
oldest first. Does not mark messages as read on Telegram.

JSON: `[Message, ...]`

### 7.8 `messages search`

```
telegram-cli messages search <chat> <query> [--limit 20]
```

Server-side text search inside one chat (`messages.search`). Prints up to `--limit`
(max 1000) matches, oldest first.

JSON: `[Message, ...]`

### 7.9 `send`

```
telegram-cli send <chat> [text] [--reply-to <msg-id>]
```

Sends plain text. If `text` is omitted or `-`, reads all of stdin. Empty text exits 2.
Text longer than 4096 characters is rejected by Telegram (`MESSAGE_TOO_LONG`, exit 1); the
CLI does not split it.

```
$ telegram-cli send @alice "lunch?"
Sent message 1235 to Alice (@alice)
```

JSON: `{"id": 1235, "chat_id": 123456789}`

### 7.10 `upload`

```
telegram-cli upload <chat> <file>... [--caption <text>] [--as auto|file|photo|video]
```

Uploads each file and sends it as its own message, in argument order. Details in
section 8.1.

```
$ telegram-cli upload me report.pdf cat.jpg --caption "for later"
report.pdf  2.1 MiB  sent as file   message 1236
cat.jpg     412 KiB  sent as photo  message 1237
```

JSON: `[{"path": "report.pdf", "size": 2202010, "as": "file", "id": 1236, "chat_id": 123456789}, ...]`

### 7.11 `download`

```
telegram-cli download <chat> <msg-id>... [--dir <dir>] [--overwrite]
```

Downloads the media of each listed message. Details in section 8.2.

```
$ telegram-cli download @golang 1234 1240
saved    ./1234_photo5012_2025-11-16_14-02-05.jpg  178.9 KiB
skipped  ./1240_slides.pdf  (exists)
```

JSON: `[{"message_id": 1234, "status": "saved", "path": "./1234_photo….jpg", "size": 183244},
{"message_id": 1240, "status": "skipped", "path": "./1240_slides.pdf"},
{"message_id": 1250, "status": "error", "error": "message has no media"}]`

### 7.12 `export`

```
telegram-cli export <chat> [--format json|md] [--output <path>] [--since <date>]
                    [--until <date>] [--limit N] [--media]
```

Writes a chat's history to one file. Details in section 9.

```
$ telegram-cli export @golang --format md --since 2025-11-01 --media
Exported 312 messages (41 files) to go-developers.md
```

JSON (summary to stdout; the export itself goes to the file):
`{"output": "go-developers.md", "format": "md", "messages": 312, "files": 41, "media_dir": "go-developers_media"}`

### 7.13 `watch`

```
telegram-cli watch [<chat>...]
```

Streams new messages (incoming and outgoing) until Ctrl-C. With chat arguments, only
those chats are shown. Starts from "now": no catch-up of messages missed while not
running. Readable output uses the message format from section 6 with a chat-name header
line; `--json` writes one `Message` object per line. Ctrl-C exits 0 here (not 130), since
it is the normal way to stop.

## 8. Upload and download

### 8.1 Upload

- Each path must be a regular file; directories and missing files exit 2 before anything
  is uploaded.
- Size limit checked before upload: 2,097,152,000 bytes (4000 parts × 512 KiB). For
  Premium accounts (`tg.User.Premium`) the limit is 4,194,304,000 bytes. Larger files exit
  2 with `file too large (limit 2000 MiB)`.
- `--as auto` (default): `.jpg`, `.jpeg`, `.png`, `.webp` files of at most 10 MiB are sent
  as photos; everything else as a file (document) with its base name and a MIME type from
  the extension. `--as photo` fails (exit 2) for files over 10 MiB. `--as video` sends a
  streamable video document; the CLI does not probe duration or resolution.
- `--caption` applies to every file in the command, as plain text.
- Files upload one at a time, in order. The first failure stops the command (exit 1);
  earlier files stay sent.
- Upload uses `uploader.NewUploader(api).WithProgress(...)`; gotd picks small or big file
  parts and the part size.

### 8.2 Download

- Destination dir: `--dir`, else `download_dir` from config, else `.`. Created with 0755 if
  missing. Downloaded files are written with 0644 (they are user content, not secrets).
- File name: `<message-id>_<name>`, where `<name>` comes from gotd
  `messages.Elem.File()` (document file-name attribute, or a generated `photo<id>_<date>.jpg`
  / `doc<id>_<date><ext>`). The name from Telegram is untrusted: the CLI keeps only
  `filepath.Base`, replaces path separators, NUL, and control characters with `_`, and
  falls back to `file` if the result is empty, `.` or `..`.
- Overwrite rule: if the target exists, the file is skipped and reported as `skipped`
  (exit 0). `--overwrite` replaces it.
- Data is written to `<target>.part` and renamed to `<target>` only after a complete
  download. On failure or Ctrl-C the `.part` file is removed.
- Photos download the largest size. Documents download the full file.
- Every listed message is attempted. Messages that do not exist or have no downloadable
  media are reported as `error`; the command exits 1 if any message failed.
- No size limit on download.

### 8.3 Progress

When stderr is a terminal, uploads and downloads show one updating line per file on
stderr, refreshed at most 10 times per second:

```
cat.jpg  [=========>          ]  212.0 KiB / 412.0 KiB  51%
```

When stderr is not a terminal, nothing is printed until each file finishes. Progress never
goes to stdout, so `--json` output stays clean.

## 9. Export

- Formats: `json` (default) and `md` (Markdown).
- Output path: `--output`, else `<slug>.<json|md>` in the current dir, where `<slug>` is the
  chat username or title lowercased, with runs of non `[a-z0-9]` characters replaced by
  `-` (fallback: `chat-<id>`). An existing output file is replaced (the export is a
  snapshot); it is written to a temp file and renamed at the end, so a failed export never
  leaves a half-written file.
- `--since` / `--until`: `YYYY-MM-DD` (local midnight; `--until` includes that whole day)
  or RFC 3339. `--since` after `--until` exits 2.
- `--limit N`: keep the newest N messages in the range. 0 (default) means all.
- Messages are fetched newest first with `messages.getHistory` (offset date = `--until`),
  stopping at `--since` or the limit, then written oldest first.
  The whole range is held in memory before writing.
- `--media`: downloads attachments into `<output-stem>_media/` next to the output file,
  using the download naming and sanitizing rules from 8.2. Existing files are skipped, so a
  re-run only fetches what is missing. Because chats can be private, the media directory
  is created 0700 and its files are written 0600, like the export file (unlike `download`). A failed attachment download is a warning on stderr,
  leaves `file` unset for that message, and does not fail the export.

JSON format:

```json
{
  "chat": Chat,
  "exported_at": "2025-11-16T15:00:00+07:00",
  "since": "2025-11-01T00:00:00+07:00",
  "until": null,
  "messages": [Message, ...]
}
```

Markdown format:

```markdown
# Go Developers (@golang)

Exported 2025-11-16 15:00 · 312 messages · 2025-11-01 to 2025-11-16

## 2025-11-16

**Alice** · 14:02 · #1234 · reply to #1200

Hello there

![photo](go-developers_media/1234_photo5012_2025-11-16_14-02-05.jpg)

**Bob** · 14:05 · #1235

[document: slides.pdf, 2.1 MiB]
```

Rules: one `##` heading per local calendar day; message text is written as-is (not
escaped); images use `![...](path)`, other files use `[name](path)` when downloaded, and
`[type: name, size]` when not; service messages are written as `*Alice: MessageActionChatAddUser*`.

## 10. Error handling

### 10.1 FLOOD_WAIT

A `telegram.Middleware` installed in `telegram.Options.Middlewares` (so it also covers file
DC requests) handles `FLOOD_WAIT_N` and `FLOOD_PREMIUM_WAIT_N` (`tgerr.AsFloodWait`):

- If the wait is at most `max_flood_wait`, it prints `rate limited by Telegram; waiting
  12s` to stderr, sleeps with `tgerr.FloodWait` (wait + 1 s, aborts on context cancel), and
  retries the same request. At most 5 retries per request.
- If the wait is longer, or retries run out, it returns a `FloodWaitError` with the
  duration → exit 6 with `Telegram asks to wait 1h2m0s before retrying this request`.
- The middleware takes a `clock.Clock` so tests can drive time with `neo.NewTime`.

### 10.2 Auth required

- No `session.json` → exit 4 without touching the network.
- After connecting, every network command except `auth login`/`auth logout` calls
  `peers.Manager.Init` (fetches self). An error matching `auth.IsUnauthorized` (HTTP-style
  401: `AUTH_KEY_UNREGISTERED`, `SESSION_REVOKED`, …) → exit 4 with `not logged in; run
  "telegram-cli auth login"`. Any 401 later in the command maps to exit 4 too.

### 10.3 Invalid peer

See section 5. The message always echoes the identifier the user typed.

### 10.4 Other

- `API_ID_INVALID` → exit 3 with `Telegram rejected the API ID/hash; check
  https://my.telegram.org`.
- Network or DC connection failures → exit 1. gotd's internal reconnects are not extended
  by the CLI.
- Ctrl-C cancels the root context (`signal.NotifyContext`); in-flight downloads clean up
  their `.part` files; exit 130.

## 11. Security rules

1. Never print, log, or include in errors: the API hash, the session file contents, auth
   keys, login codes, or 2FA passwords. The API ID may be printed.
2. Typed secrets are read with hidden input on terminals. The API hash and password are
   never accepted as flags.
3. Secret files: 0600 in a 0700 directory, written atomically; permissions tightened on
   start (section 3.1).
4. `auth logout` removes `session.json` and `peers.json`.
5. Never commit secrets. `.gitignore` must cover `*.session`, `.env`, `session.json`,
   `credentials.json`, and `peers.json` (phase 1 adds the last three). Tests use temp dirs (`t.TempDir()` with
   `HOME`/`XDG_*` set via `t.Setenv`) and never real credentials.
6. gotd's `Options.Logger` stays unset (no logs). Errors are printed as returned, but the
   CLI never formats credentials or session data into its own error messages, and tests
   assert that the API hash never appears in stdout or stderr.
7. File names from Telegram are sanitized before touching the file system (section 8.2).
8. Contacts' phone numbers are never written to disk (section 3.4).

## 12. Package layout

```
main.go                 calls cmd.Execute()
cmd/                    cobra commands, flag parsing, prompts, exit-code mapping
  root.go               root command, --json, --version, Execute, error → exit code
  init.go auth.go me.go chats.go messages.go send.go upload.go download.go export.go watch.go
internal/config/        paths (XDG), config.toml, credentials.json, env precedence, perms,
                        atomic file writes
internal/tgclient/      telegram.Client construction, session storage, flood-wait
                        middleware, peers.json storage, chat resolution, auth flow
                        authenticator, upload/download helpers
internal/output/        JSON view models (User, Chat, Message), readable printers,
                        progress line, size/time formatting
internal/export/        JSON and Markdown export writers
```

Rules: `cmd` holds no Telegram logic beyond wiring; logic that takes a `*tg.Client` lives in
`internal/tgclient` or `internal/export` so it can be tested with `tgmock`. No other
internal packages unless a phase proves one is needed.

New dependencies: `github.com/BurntSushi/toml` (config) and `golang.org/x/term` (hidden
input, TTY detection). Tests use `github.com/stretchr/testify` (already required by
`tgmock`). Nothing else.

## 13. gotd/td API map (verified against v0.166.1)

Every symbol below was checked in the module source of `github.com/gotd/td@v0.166.1`, the
version pinned in `go.mod`. Re-verify if the pin changes.

| Need | gotd API |
|------|----------|
| Client | `telegram.NewClient(appID int, appHash string, telegram.Options)`, `(*Client).Run(ctx, func(ctx) error)`, `(*Client).API() *tg.Client` |
| Options used | `Options.SessionStorage`, `Options.Middlewares`, `Options.UpdateHandler`, `Options.NoUpdates`, `Options.Device` (`telegram.DeviceConfig{DeviceModel, SystemVersion, AppVersion}`) |
| Session file | `session.FileStorage{Path: ...}` (writes 0600 via `os.WriteFile`; missing file → `session.ErrNotFound`, treated as a new session) |
| Auth status | `(*Client).Auth().Status(ctx)` → `*auth.Status{Authorized, User}`; `auth.IsUnauthorized(err)` (401) |
| Login | `auth.NewFlow(auth.UserAuthenticator, auth.SendCodeOptions{})`, `(*auth.Client).IfNecessary(ctx, flow)`; our authenticator implements `Phone`, `Password`, `Code(ctx, *tg.AuthSentCode)`, `AcceptTermsOfService`, `SignUp`; `auth.ErrPasswordAuthNeeded`; `auth.FlowClient` for tests |
| Logout | `(*Client).LogOut(ctx)` (treats 401 as success, wipes the stored session) |
| Self | `(*Client).Self(ctx)`, `peers.Manager.Self(ctx)` |
| Peers | `peers.Options{Storage, Cache}.Build(api)`, `(*peers.Manager).Init`, `Resolve`, `ResolveTDLibID`, `Apply(ctx, users, chats)`, `UpdateHook`; `peers.Storage` interface (`Save`, `Find`, `SavePhone`, `FindPhone`, `GetContactsHash`, `SaveContactsHash`); `peers.PeerNotFoundError`, `peers.PhoneNotFoundError`; `peers.Peer.InputPeer()`, `TDLibPeerID()`, `VisibleName()`, `Username()` |
| IDs | `constant.TDLibPeerID` (`User`, `Chat`, `Channel`, `IsUser`, `IsChat`, `IsChannel`, `ToPlain`) |
| Dialogs | `query.GetDialogs(api).BatchSize(n).Iter()` → `dialogs.Elem{Dialog, Peer, Last, Entities}` |
| History | `query.Messages(api).GetHistory(inputPeer).OffsetID(id).OffsetDate(unix).BatchSize(n).Iter()` → `messages.Elem{Msg, Peer, Entities}` |
| Search | `query.Messages(api).Search(inputPeer).Q(text).Iter()` |
| By ID | `query.Messages(api).GetMessagesWithEntities(ctx, inputPeer, ids...)` (handles channels) |
| Media info | `messages.Elem.File()` → `messages.File{Name, MIMEType, Location}`, `Elem.Document()`, `Elem.Photo()` |
| Send | `message.NewSender(api).To(inputPeer).Reply(id).Text(ctx, s)`; `unpack.MessageID(updates, err)` (`telegram/message/unpack`) |
| Upload | `uploader.NewUploader(api).WithProgress(uploader.Progress)`, `FromPath(ctx, path)`; `uploader.ProgressState{Uploaded, Total}`; `message.UploadedPhoto(file, caption...)`, `message.UploadedDocument(file, caption...).Filename(n)`, `.Video().SupportsStreaming()`; `Builder.Media(ctx, opt)`; `styling.Plain(s)` |
| Download | `downloader.NewDownloader().Download(api, loc).WithSize(n).WithProgress(func(done, total int64)).ToPath(ctx, path)`; `client.API()` follows `FILE_MIGRATE` to the right DC |
| Flood wait | `tgerr.AsFloodWait(err)`, `tgerr.FloodWait(ctx, err, tgerr.FloodWaitWithClock(c))`, `telegram.MiddlewareFunc`, `telegram.InvokeFunc` |
| Errors | `tgerr.Is(err, ...)`, `tg.ErrAPIIDInvalid`, `tg.ErrPeerIDInvalid`, `tg.ErrChannelPrivate`, `tg.ErrChannelInvalid`, `tg.ErrUsernameNotOccupied`, `tg.ErrUsernameInvalid` |
| Updates | `tg.NewUpdateDispatcher()`, `OnNewMessage`, `OnNewChannelMessage`; `updates.New(updates.Config{Handler, AccessHasher})`; `(*updates.Manager).Run(ctx, api, selfID, updates.AuthOptions{Forget: true, OnStart})` |
| Test helpers | `tgmock.New(t)` / `tgmock.NewRequire(t)` as `tg.Invoker`, `tg.NewClient(mock)`, `ExpectCall(req).ThenResult(res)`, `ThenFlood(n)`, `ThenUnregistered()`, `ThenRPCErr(err)`; `neo.NewTime` as `clock.Clock` |

Notes found while verifying:

- `peers.Manager.Resolve` treats strings of digits as phone numbers; section 5 handles
  numeric IDs first for that reason.
- `peers.Manager`'s default storage is in-memory; without our `peers.json` storage,
  numeric IDs would stop resolving between runs.
- `session.FileStorage` only sets 0600 when it creates the file, and does not create the
  directory; `internal/config` creates the 0700 dir and fixes modes.
- `go.sum` in the initial commit lacks gotd's transitive sums; phase 1 runs `go mod tidy`.
