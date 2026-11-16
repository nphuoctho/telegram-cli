# telegram-cli build plan

Build order for [spec.md](spec.md). Six phases. Each phase is one local branch off `main`,
merged before the next starts, and leaves a working CLI: the commands it adds work end to
end, and the commands it does not add yet are absent (not stubs).

## Rules for every phase

- Branch names: `feat/p1-config`, `feat/p2-auth`, `feat/p3-read`, `feat/p4-send-files`,
  `feat/p5-export`, `feat/p6-watch`. Conventional Commits messages.
- Gate before merge: `go build ./...`, `go vet ./...`, `gofmt -l .` prints nothing,
  `go test ./...` passes with networking disabled (tests must not dial Telegram).
- Tests never use real credentials or `~`: they set `HOME`, `XDG_CONFIG_HOME`, and
  `XDG_DATA_HOME` to `t.TempDir()` with `t.Setenv`.
- Telegram is faked at the RPC layer: `tgmock.New(t)` is a `tg.Invoker`, and
  `tg.NewClient(mock)` gives the `*tg.Client` that our code takes. Logic that needs Telegram
  takes a `*tg.Client` (or a gotd interface such as `auth.FlowClient`, `uploader.Client`,
  `downloader.Client`), never a `*telegram.Client`, so it can be tested this way.
- Command tests run the cobra command with `SetArgs`, `SetIn`, `SetOut`, `SetErr` and check
  stdout, stderr, and the mapped exit code.
- `cmd` reaches Telegram only through one package-level seam added in phase 2:
  `var runTelegram = tgclient.Run` with signature
  `func(ctx context.Context, cfg config.Resolved, fn func(context.Context, *tgclient.Session) error) error`,
  where `Session` holds `API *tg.Client` and `Peers *peers.Manager`. Tests replace
  `runTelegram` with a function that builds a `Session` from `tgmock`.
- Manual check before merge: run the phase's commands against a real account and paste
  the transcript (with IDs and names redacted) into the merge note. Never commit it.
- Re-verify any new gotd symbol against the module source of the pinned version before
  using it; add it to spec section 13 if it is not there.

## Phase 1: setup and config

Branch `feat/p1-config`.

Commands: root (`--json`, `--version`), `init`.

Work:

- `go mod tidy` (adds missing gotd sums), add `github.com/BurntSushi/toml` and
  `golang.org/x/term`.
- `.gitignore`: add `session.json`, `credentials.json`, `peers.json`.
- `internal/config`: path resolution (XDG with `$HOME` fallback), `config.toml` load with
  unknown-key rejection and defaults, `credentials.json` load/save with validation, env
  overrides, permission tightening, atomic write helper (temp file + rename, 0600).
- `internal/output`: JSON vs readable switch, error printing (readable and JSON), size
  formatting.
- `cmd/root.go`: persistent `--json`, `--version` from `debug.ReadBuildInfo`, typed errors
  mapped to exit codes 0/1/2/3/130 (codes 4–6 are added with the code that raises them),
  `signal.NotifyContext` for SIGINT/SIGTERM.
- `cmd/init.go` as in spec 7.1.

Acceptance:

- `telegram-cli init` with piped input `1234567\n0123456789abcdef0123456789abcdef\n` creates
  the data dir (0700), `credentials.json` (0600) with that content, and `config.toml` with
  defaults; prints both paths.
- Running it again exits 2 and leaves the file unchanged; with `--force` it overwrites.
- Bad API ID (`abc`, `0`, `-1`) or hash (31 chars, uppercase, non-hex) exits 2 and writes
  nothing.
- `--json` variant prints the documented JSON object and nothing else on stdout.
- A `config.toml` with an unknown key exits 2 naming the key.
- `telegram-cli --version` prints a version line.
- `go install ./...` produces a binary named `telegram-cli`.

Tests:

- `internal/config`: path resolution with and without `XDG_*`; relative `XDG_*` ignored;
  defaults when files are missing; env overrides each field separately; missing value →
  not-configured error; credentials validation table; unknown config key; permission
  tightening of a 0644 file and 0755 dir (skip on Windows); atomic write leaves no temp file.
- `cmd`: `init` happy path, refusal without `--force`, invalid input, JSON output;
  exit-code mapping table for each typed error; JSON error shape on stderr.
- Security: after `init`, neither stdout nor stderr contains the API hash.

## Phase 2: auth

Branch `feat/p2-auth`.

Commands: `auth login`, `auth logout`, `auth status`, `me`.

Work:

- `internal/tgclient`: `Run` builds `telegram.NewClient` with `session.FileStorage` at
  `<data dir>/session.json`, `NoUpdates: true`, `Device` set to `telegram-cli` + version,
  and the flood-wait middleware; builds `Session{API, Peers}` with
  `peers.Options{}.Build(api)` (in-memory storage for now) and runs the callback inside
  `Client.Run`.
- Flood-wait middleware per spec 10.1, with injected `clock.Clock` and stderr writer;
  `FloodWaitError` → exit 6.
- Terminal authenticator implementing `auth.UserAuthenticator` (prompts on stderr, hidden
  password input, line reads when not a TTY, `SignUp` returns the "sign up in an official
  app" error).
- Auth check per spec 10.2 (no session file → exit 4 before connecting; `Peers.Init`
  returning 401 → exit 4); `API_ID_INVALID` → exit 3.
- `output.User` view model and readable printer.
- Commands per spec 7.2–7.5.

Acceptance:

- Against a real account: `auth login` with phone + code (+ 2FA if set) succeeds, then
  `auth status` and `me` show the account; `auth logout` removes `session.json`, and
  `auth status` then exits 4.
- `me` with no session file exits 4 immediately without network access.
- With `TELEGRAM_APP_ID`/`TELEGRAM_APP_HASH` set and no credentials file, `auth status`
  reports `env`.
- `auth login` on an authorized session prints the status without prompting.
- `session.json` is 0600 after login.

Tests:

- Flood-wait middleware with `tgmock` (`ThenFlood(3)` then a result) and `neo.NewTime`:
  retries after the wait; wait above `max_flood_wait` returns `FloodWaitError` without
  sleeping; gives up after 5 retries; context cancel during the wait returns promptly.
- Authenticator: feeds code and password from a reader; `auth.NewFlow(...).Run` against a
  fake `auth.FlowClient` covering code-only, code + 2FA (`auth.ErrPasswordAuthNeeded`), and
  sign-up-required.
- `auth status`/`me` via the `runTelegram` seam with `tgmock` returning a `tg.User`
  (`users.getUsers` for self): readable and JSON output; `ThenUnregistered()` → exit 4.
- `auth logout`: deletes `session.json` and `peers.json` on success and when the RPC fails
  (then exit 1); no session file → "Not logged in", exit 0.
- Security: login test asserts the code and password never appear on stdout/stderr.

## Phase 3: read

Branch `feat/p3-read`.

Commands: `chats list`, `messages read`, `messages search`.

Work:

- `peers.json` storage implementing `peers.Storage` (access hashes persisted; phones and
  contacts hash in memory only), loaded at start and saved once at the end of the command
  if changed.
- `Session.Peers` switches to `peers.Options{Storage: store}.Build(api)`.
- Chat resolver per spec 5, including the one-time dialog walk fallback and exit 5
  mapping.
- `output.Chat` and `output.Message` view models (from `dialogs.Elem` / `messages.Elem`),
  readable printers, media type mapping.
- Commands per spec 7.6–7.8.

Acceptance:

- `chats list` shows real chats with Bot-API IDs; `--limit 0` lists all.
- `messages read <id>` works for an ID from `chats list` in a fresh process (hash from
  `peers.json`), and for `@username`, a t.me link, and `me`.
- Unknown `@nosuchuser123456` and unknown numeric ID exit 5 with the typed identifier in
  the message.
- `messages search me <word>` finds a message you saved earlier.
- Output is oldest first in both modes; `--json` matches the shapes in spec 6.

Tests:

- Peer store: round trip through the file; 0600 mode; phones not written; corrupt file →
  clear error (exit 1), not a silent reset.
- Resolver table with `tgmock`: `me`; positive, negative, and `-100` IDs found in the store;
  ID missing from the store then found by the dialog walk; ID never found → exit 5;
  `@name` → `contacts.resolveUsername`; `USERNAME_NOT_OCCUPIED` → exit 5; `+phone` is not
  parsed as an ID.
- View models: a `tg.Message` with each media kind, a reply, a channel post without
  `from`, and a `tg.MessageService` → expected `output.Message`; readable printer golden
  strings.
- `chats list`, `messages read`, `messages search` through the seam with canned
  `messages.getDialogs` / `messages.getHistory` / `messages.search` results; limit and
  `--before` honored; ordering oldest first.

## Phase 4: send, upload, download

Branch `feat/p4-send-files`.

Commands: `send`, `upload`, `download`.

Work:

- `send` with stdin input and `--reply-to`; message ID via `unpack.MessageID`.
- Upload per spec 8.1 (`uploader` + `message.UploadedPhoto` / `UploadedDocument` /
  `.Video()`), size limits, `--as` selection.
- Download per spec 8.2 (`GetMessagesWithEntities` → `Elem.File()` → `downloader` to
  `.part` → rename), file-name sanitizing, overwrite rule.
- Progress line per spec 8.3 (shared by upload and download).

Acceptance:

- `send me "hello"` posts to Saved Messages and prints the new message ID;
  `echo hi | telegram-cli send me` works.
- `upload me a.pdf b.jpg` sends a document and a photo; a 12 MiB `.jpg` goes as a file in
  auto mode and is rejected with `--as photo`; a file over the limit is rejected before any
  network call.
- `download me <id>` saves `<id>_<name>` into `download_dir`; a second run reports
  `skipped`; `--overwrite` replaces it; Ctrl-C mid-download leaves no `.part` file.
- A 50 MiB file uploads and downloads with a progress line on a terminal and no progress
  noise when stderr is redirected.

Tests:

- `send`: request built with the right peer, text, and reply ID (`tgmock.ExpectFunc`);
  ID extracted from `UpdateShortSentMessage` and from `Updates` with
  `UpdateNewChannelMessage`; empty stdin → exit 2.
- Upload kind selection table (extension × size × `--as`); size-limit table including the
  Premium limit; small-file upload through `tgmock` (`upload.saveFilePart` then
  `messages.sendMedia`); stops on first failure.
- File-name sanitizing table: `../../etc/passwd`, `a/b`, `\x00`, empty, `.`, `..`, long
  Unicode names.
- Download through `tgmock` returning `upload.file` chunks: writes the file, skips an
  existing one, overwrites with the flag, removes `.part` on an RPC error; message without
  media → per-item error and exit 1 while other items succeed.
- Progress printer: throttling with an injected clock; silent when not a terminal.

## Phase 5: export

Branch `feat/p5-export`.

Commands: `export`.

Work:

- `internal/export`: fetch loop per spec 9 (history with offset date, stop at `--since` or
  `--limit`), JSON writer, Markdown writer, atomic output write.
- `--media` reusing the phase 4 download code with skip-existing.
- Date parsing for `--since`/`--until` (local date or RFC 3339).

Acceptance:

- `export me` writes `saved-messages.json` (or the slug of your name) with all messages,
  oldest first.
- `export <chat> --format md --since 2025-11-01 --until 2025-11-07` contains only that
  week, grouped by day.
- `--media` downloads attachments into `<stem>_media/` and the export references them; a
  re-run downloads nothing new.
- An interrupted export leaves no partial output file.

Tests:

- Date parsing table, including `--until` covering the whole day and `--since` > `--until`
  → exit 2.
- Fetch loop with `tgmock` pages: stops at `--since`, honors `--limit`, handles an empty
  chat.
- JSON writer: output unmarshals into the documented shape; golden file.
- Markdown writer golden files: day headings, replies, downloaded vs not-downloaded media,
  service messages.
- Slug table (username, Unicode title, empty title → `chat-<id>`).
- Failed attachment download → warning, `file` omitted, export still succeeds.

## Phase 6: watch

Branch `feat/p6-watch`.

Commands: `watch`.

Work:

- `tgclient.Run` gains a watch mode: `UpdateHandler` = `updates.New(updates.Config{Handler:
  peers.UpdateHook(dispatcher), AccessHasher: peers})`, `NoUpdates: false`, and
  `(*updates.Manager).Run(ctx, api, selfID, updates.AuthOptions{Forget: true})`.
- `tg.NewUpdateDispatcher()` with `OnNewMessage` and `OnNewChannelMessage` converting to
  `output.Message` via the phase 3 view model; chat filter by resolved Bot-API ID.
- Readable stream and JSON Lines output; Ctrl-C exits 0.

Acceptance:

- `watch` prints a message sent to you from a phone within a few seconds, in both modes.
- `watch @somegroup` shows only that group.
- `watch --json | jq .text` works line by line.
- Ctrl-C stops cleanly with exit 0 and no error output.

Tests:

- Dispatcher handlers: `UpdateNewMessage` and `UpdateNewChannelMessage` with
  `tg.Entities` → expected `output.Message`; filter drops other chats.
- JSON Lines writer: one object per line, flushed per message.
- Command test: a fake update source feeds two updates then cancels the context; output
  has two lines and exit code 0.
