# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Dev environment

The toolchain (Go, golangci-lint, sqlite, ngrok) is provided by the Nix flake. Either `nix develop` into the shell or use `direnv allow` (the `.envrc` runs `use flake` and auto-loads `.env`). Don't try to install Go separately.

## Common commands

Run from inside `nix develop` (or with direnv loaded):

- `make check` — fmt + vet + lint + test (the full local gate)
- `make test` — `go test ./...`
- `make run` — build and run against the env vars in `.env`
- `go test ./internal/parser -run TestParse` — single package / single test
- `nix flake check` — release build with tests + `flake.nix` formatting check
- `nix build` — produce the release binary at `./result/bin/sergeant`
- `nix run .#sergeant-pl` — run the binary with `SERGEANT_LOCALE=pl` baked in

When `vendorHash` in `flake.nix` becomes stale (after `go.mod`/`go.sum` changes), `nix build` prints the expected hash; paste it into `flake.nix`.

## Architecture

Single Go binary (`cmd/sergeant`) that runs an HTTP server on `:8080` exposing `/slack/events` (and `/healthz`). Slack delivers both Events API callbacks (JSON) and Interactivity payloads (`application/x-www-form-urlencoded`) to the same URL — `slackapi.Handler.ServeHTTP` distinguishes them by `Content-Type`. Every request is verified against `SLACK_SIGNING_SECRET` before dispatch.

Request flow for a mention or DM:

1. `slackapi.Handler.dispatch` extracts text from either `AppMentionEvent` or `MessageEvent` (DMs only — `ChannelType == "im"`, no bots, no edits, not our own user). The leading `<@BOT>` mention is stripped.
2. `parser.Parse` turns the text into a `parser.Command` (one of `KindAdd`, `KindReset`, `KindStatusFor`, `KindStatusAll`, `KindPaySet`, `KindPaySetForm`, `KindPayRemove`, `KindPayClear`, `KindPayShowSelf`, `KindPayShowFor`, `KindHelp`).
3. `ledger.Ledger.Apply` executes the command against `store.SQLite` and returns a `ledger.Reply{Text, Ephemeral}`. For channel mentions, successful add/subtract/reset commands stop after a `:sergeant:` reaction; the same commands in DMs receive a text reply. Other replies are posted through the `Responder` interface according to `ledger.Reply`.

`KindPaySetForm` is the one branch that bypasses the ledger: the handler posts an ephemeral opener with a button, the button triggers `slackapi.InteractionTypeBlockActions` → `OpenView` (modal), and modal submission goes through `handlePayFormSubmission`, which must respond synchronously (empty 200 closes the modal, JSON `errors` reopens it with field-level errors, `update` swaps to a confirmation view).

The handler dispatches the actual work in a goroutine (`safely(...)` recovers panics) and returns `200 OK` immediately so Slack doesn't retry on slow requests.

### Data model (`internal/store`)

SQLite via `modernc.org/sqlite` (pure Go — no CGO). Two tables:

- `debts(creditor_id, debtor_id, currency, amount_minor, …)` — primary key is the triple. One row per (creditor, debtor, currency). Amounts are stored as int64 minor units (1/100 of major) and **clamped at zero** by `AddDelta`: subtracting more than is owed leaves the row at 0 (and then deletes it in the same tx). There's no concept of negative debt — if you "overpay", the tab just clears.
- `payment_methods(user_id, method, value, …)` — primary key `(user_id, method)`. Methods are normalized to `[a-z0-9-]{1,20}`.

`AddDelta` is a single transaction: upsert with `MAX(0, …)`, re-read, delete-if-zero, commit. Anything mutating `debts` should preserve that invariant.

### i18n (`internal/i18n`)

All user-facing strings go through `Translator.T(id, args...)` with IDs declared as constants in `i18n.go`. Bundles for `en` and `pl` live in the same file — **adding a new string means adding it to both bundles**, otherwise the Polish locale silently falls back to English. `Translator.Since(now, then)` produces locale-aware relative timestamps with proper Polish plurals (`plPlural` handles the 2/3/4 vs 5+ case). Locale is selected by `SERGEANT_LOCALE` (`en` default, `pl` available); unknown values fall back to `en`.

### Adding a new command

1. Add a `Kind*` constant and parse branch in `internal/parser/parser.go` (with table-driven tests in `parser_test.go`).
2. Add the `case` in `ledger.Ledger.Apply`. If it touches storage, add the `SQLite` method first.
3. Add message IDs + `en`/`pl` translations in `internal/i18n/i18n.go`.
4. If the command is interactive (modal/buttons), wire it through `internal/slackapi/views.go` and the interactivity branch of `Handler.ServeHTTP`.

## Conventions

- Tests are colocated (`*_test.go`); the parser, ledger, store, i18n, and handler all have their own test files. Prefer extending those.
- Logging is `slog` JSON to stdout. `LOG_LEVEL=debug` gets you the parsed-text and dispatch lines.
- For local Slack testing, run `ngrok http 8080` and point the Slack app's Event Subscriptions + Interactivity URLs at `https://<ngrok>/slack/events`.
