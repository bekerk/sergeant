# sergeant

<p align="center">
  <img width="40%" src="assets/sergeant.png" />
</p>

A Slack bot that tracks who owes you money.

## Talking to the bot

```
@sergeant @jan +20 PLN     jan now owes you 20 PLN
@sergeant @jan 20 PLN      plus is optional!
@sergeant @jan -5          knocks 5 off his tab (default currency)
@sergeant @jan reset       clears his tab with you
@sergeant @jan status      shows what he owes you (or @sergeant @jan ?)
@sergeant status           shows everyone's tab with you (or @sergeant ?)

@sergeant pay set          opens a private form to add a payment method
@sergeant pay set bank PL61 1090 ...   same, but typed inline
@sergeant pay set-default bank         marks a saved method as your default
@sergeant pay me           shows your own payment methods
@sergeant @jan pay         shows jan's payment methods
@sergeant pay rm bank      removes one method
@sergeant pay clear        wipes all your methods

@sergeant help             shows the help message (private to you)
@sergeant hello            posts the help message to the channel
```

Debt changes use reactions only, including in DMs: `:sergeant:` on success and `:sergeant-no:` on failure. Status and pay replies are private to you.

You can also DM the bot directly - drop the `@sergeant` prefix and just type the command (e.g. `@jan +20 PLN`).

## Running it

For dev:

```sh
cp .env.example .env        # then fill in SLACK_BOT_TOKEN / SLACK_SIGNING_SECRET
nix develop                 # or `direnv allow` if you use direnv (auto-loads .env)
make run
```

One-shot run from the flake:

```sh
SLACK_BOT_TOKEN=xoxb-... SLACK_SIGNING_SECRET=... nix run
# PL:
SLACK_BOT_TOKEN=xoxb-... SLACK_SIGNING_SECRET=... nix run .#sergeant-pl
```

It listens on `:8080` and expects Slack events at `/slack/events`. For local dev, point `ngrok http 8080` at it.

## Slack setup

In your Slack app:

- OAuth scopes: `app_mentions:read`, `chat:write`, `chat:write.public`, `im:history` (for DMs).
- Event Subscriptions: enable, request URL `https://your-host/slack/events`, subscribe to `app_mention` and `message.im`.
- Interactivity & Shortcuts: enable, request URL `https://your-host/slack/events`.

## Env vars

- `SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET` - required
- `SERGEANT_DB_PATH` - defaults to `./sergeant.db`
- `SERGEANT_HTTP_ADDR` - defaults to `:8080`
- `SERGEANT_DEFAULT_CURRENCY` - defaults to `PLN`
- `SERGEANT_LOCALE` - `en` or `pl`, defaults to `en`

## Tests / Linting / Whatever

From inside `nix develop`:

```sh
make check        # fmt + vet + lint + test
make test         # just tests
```

`nix flake check` runs the release build (with tests) and verifies `flake.nix` formatting.
