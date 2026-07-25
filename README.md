# reading-bot

A Telegram bot I message with reading updates. It commits directly to the
markdown repo behind my personal site, so "started The Testaments" becomes a
published page a minute later.

Two jobs:

1. **New book**: send a title, the bot looks up metadata, shows what it found,
   and on confirmation creates a note.
2. **Update a book**: reference one that already exists, get buttons: mark
   finished, mark abandoned, add a rating, append a dated progress note.

Work in progress. Nothing below the config loader is built yet.

## Design

Three decisions worth stating up front, because they're most of why this is
small:

**Long polling, not webhooks.** Telegram's `getUpdates` means the bot only makes
outbound connections: no inbound port, no TLS certificate, no reverse proxy, no
subdomain. The deployment is one static binary and a systemd unit.

**The GitHub Contents API, not a git clone.** The bot reads a file (getting its
blob SHA), writes the new content, and commits in one API call. The host stays
stateless, there are no SSH keys on it, and the bot never has to resolve a
merge. The cost is that one edit is one commit is one Pages deploy.

**Inline keyboards, not free-text parsing.** This gets used from a phone.
Parsing "finished the testaments 8/10" is fragile and irritating to debug. Send
a title, get buttons.

## Configuration

Everything comes from the environment. There is no config file in this repo, so
there's nothing to accidentally commit.

| Variable | Required | Default | |
|---|---|---|---|
| `TELEGRAM_BOT_TOKEN` | yes | | From @BotFather. |
| `TELEGRAM_ALLOWED_USER_ID` | yes | | Numeric user ID. Every other sender is ignored. |
| `GITHUB_TOKEN` | yes | | Fine-grained PAT, one repo, contents read/write. |
| `GITHUB_OWNER` | yes | | Owner of the content repo. |
| `GITHUB_REPO` | yes | | The content repo. |
| `GITHUB_BRANCH` | no | `v4` | Branch to commit to. |
| `CONTENT_DIR` | no | `content/reading` | Directory holding the notes. |
| `DRY_RUN` | no | `true` | When on, no GitHub writes, the bot replies with the file content it would have written. |

A missing or malformed variable fails the process at startup, and reports every
problem at once rather than one per restart.

## Running locally

```bash
cp .env.example .env   # then fill it in
set -a; source .env; set +a
go run ./cmd/reading-bot
```

```bash
go test ./...
```

`DRY_RUN` defaults to on. Turn it off deliberately.

## A note on test fixtures

The site's content lives in a **private** repo, and some notes are marked
`private: true`, deliberately not published. This repo is public.

So: fixtures under any `testdata/` directory are **synthetic**. Invent the
books. Never copy real notes out of the vault, and never point a test at the
real repo. The code under test cares about YAML and body appends, not about
whether the prose is real.

## License

MIT.
