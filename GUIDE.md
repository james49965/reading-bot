# Building a Telegram reading bot in Go: a learning guide

This is a build-it-yourself guide for a small, real project: a Telegram bot you
message with reading updates, which commits directly to the markdown repo behind
a [Quartz](https://quartz.jzhao.xyz/) site. Tell it "started The Testaments" and
a published page appears a minute later.

It assumes you can program but have never written Go and have never built a
Telegram bot. It's written for someone coming from C#/.NET, so there are
"coming from .NET" notes where Go's habits differ sharply.

**The guide does not contain the finished code.** Each phase tells you what to
learn, what to build, and how to know it works. Where a Go idiom is
unguessable — struct tags, the time format, the HTTP client pattern — it's shown
outright, because memorising trivia isn't the lesson. Where the design decision
*is* the lesson, you get a signature and a failing test.

---

## The finished thing

Two jobs:

1. **New book.** You send a title. The bot looks up metadata, shows you what it
   found, and on confirmation creates a note in `content/reading/`.
2. **Update a book.** You reference one that exists. The bot offers buttons:
   mark finished, mark abandoned, add a rating, append a dated progress note.

It runs permanently on a cheap VPS as a single static binary under systemd.

### Three design decisions, made in advance

You should understand *why* before you build, because each one removes a whole
category of work.

**Long polling, not webhooks.** Telegram offers two ways to receive messages.
A webhook means Telegram makes an inbound HTTP request to you — which needs a
public IP, an open port, a TLS certificate, a domain name, and a reverse proxy.
Long polling means *you* call Telegram's `getUpdates` and it holds the
connection open until something happens. Only outbound connections. The
deployment collapses to one binary and one systemd unit. The cost is latency
measured in a second or two, which for a personal bot is nothing.

**The GitHub Contents API, not a git clone on the server.** The obvious approach
is to clone the repo onto the VPS and `git commit && git push`. Don't. That puts
an SSH key on the box, requires the disk to stay in sync, and means the bot must
eventually resolve a merge conflict — at 11pm, unsupervised, against your
website. Instead, the GitHub REST API lets you read a file (getting its blob
SHA) and write it back in one call, committing server-side. The VPS holds no
state. The cost: one edit is one commit is one deploy, and you can't write two
files atomically.

**Inline keyboards, not natural language.** You'll use this from a phone.
Parsing "finished the testaments 8/10" is fragile, and debugging it via text
message is miserable. Send a title, get buttons. Minimal typing, no ambiguity.

### The contract with the site

The bot writes files that must look exactly like this:

```markdown
---
title: The Testaments
created: "2026-07-25"
status: reading
author: Margaret Atwood
summary: Picked it up straight after The Handmaid's Tale, which was a 10/10.
link:
draft: false
tags:
  - reading
  - fiction
---

Started this off the back of the TV series.

## Take

_Still reading._
```

Four rules that will bite you:

- **`created` must be a quoted `YYYY-MM-DD` string.** If it's missing or not a
  string, the page silently shows the site's *build* date instead. This looks
  fine locally and only misbehaves once deployed.
- **`status` must be exactly `reading`, `finished`, or `abandoned`.** A custom
  Quartz component groups the reading index on this value. Anything else and the
  book vanishes from the page.
- **`summary` is the card text on the index.** Leave it blank and Quartz
  auto-extracts the first sentence, which reads badly. Always set something.
- **The filename is a kebab-case slug** with punctuation stripped:
  `The Handmaid's Tale` → `the-handmaids-tale.md`.

---

## Phase 0 — Enough Go to start

**Goal:** not fluency. Enough to read the scaffold without guessing.

Work through [A Tour of Go](https://go.dev/tour/), but only these:

- **Basics** — packages, imports, functions, variables, `:=`, zero values.
- **Methods and interfaces** — stop after "Stringers". This is the section that
  matters most and the one that will feel strangest.
- **Errors** — short, essential.

**Skip for now:** goroutines, channels, generics. You'll meet goroutines briefly
in Phase 8 and won't need the rest.

### Coming from .NET

| C# | Go |
|---|---|
| `public` / `private` | Capitalised name is exported, lowercase isn't. It's the *identifier*, not a keyword. |
| `throw` / `try-catch` | No exceptions. Functions return `(value, error)` and you check it every time. |
| `class Foo : IBar` | Interfaces are satisfied **implicitly**. A type never declares that it implements one. |
| `null` | Zero values. An unset `string` is `""`, an `int` is `0`, a struct is fully-formed with zeroed fields. |
| LINQ | Nothing like it. You write the loop. This is fine. |
| `async`/`await` | Not needed here. Go's I/O is blocking; concurrency is goroutines. |

The two that will trip you most: **errors are values you must handle explicitly**
(Go's `if err != nil` is famously verbose and it's the correct trade), and
**implicit interfaces** — you define an interface where it's *consumed*, not
where it's implemented. That's backwards from C# and it's what makes Go easy to
test, as you'll see in Phase 5.

### Tooling

```bash
go run ./cmd/reading-bot     # compile and run
go test ./...                # run every test
go vet ./...                 # catch likely mistakes
gofmt -w .                   # format (there is one style; don't argue with it)
go doc net/http Client       # read docs offline
```

Run these from the terminal at least a few times even if you use an IDE. When
something breaks on the VPS, the terminal is all you'll have.

### If you're using GoLand

It'll feel like Rider, because it is. Four things worth setting up now:

- **A run configuration with environment variables.** Run → Edit Configurations
  → Go Build, point it at `./cmd/reading-bot`, and put your variables in the
  *Environment* field. **This is a secrets hazard:** GoLand stores run
  configurations under `.idea/`, so your bot token ends up in a file in the
  repo. `.idea/` is gitignored here for exactly that reason — leave it that way.
  Better still, install the **EnvFile** plugin and point it at `.env`, so the
  token lives in one gitignored place instead of two.
- **Green arrows in the test gutter** run a single test, and `t.Run` subtests
  get their own arrow. This makes the table-driven tests in Phase 3 genuinely
  pleasant — add a row, run just that row.
- **Format on save.** Settings → Tools → Actions on Save → tick *Reformat code*
  and *Optimize imports*. Go has exactly one correct format and GoLand knows it,
  so stop thinking about it permanently.
- **The debugger works properly** — breakpoints, stepping, variable inspection,
  same as Rider. Worth using in Phase 2 to look at a real `Update` struct and
  see which fields are nil. That's a faster way to understand Telegram's shape
  than reading their docs.

Two habits from .NET that don't carry over: you won't need to configure a build
system (`go build` is the whole thing), and GoLand's "implement interface"
tooling matters much less here, because Go interfaces are satisfied implicitly —
there's nothing to generate.

**Done when:** you can explain what `func (c *Config) Redacted() string` means —
every part of it, including why there's a `*`.

---

## Phase 1 — Read the scaffold you already have

**Goal:** the existing code stops being someone else's.

The repo already contains a working config loader. Read `internal/config/config.go`
and `internal/config/config_test.go` and answer these, out loud:

1. Why is the package under `internal/`? (Hint: `go doc` won't tell you — search
   "go internal packages". It's a compiler-enforced visibility rule.)
2. `func (l *loader) required(key string) string` — why is the receiver `*loader`
   and not `loader`? What would break with a value receiver?
3. `Load()` collects problems in a slice and returns them all at once. What's the
   alternative, and why is it worse for something running under systemd?
4. `boolWithDefault` treats an unparseable `DRY_RUN=yes` as an *error* rather
   than as `false`. Argue for that choice in one sentence.
5. In the test file, why does `setEnv` clear every key before setting any?

Then break it deliberately: change `DRY_RUN`'s default to `false`, run
`go test ./...`, and read the failure. Change it back. Getting comfortable
reading Go test output now saves you an hour later.

**Done when:** you can answer all five, and `go test ./...` passes.

---

## Phase 2 — A bot that talks to you and nobody else

**Goal:** first contact with the Telegram API. No site writes yet.

### How the Telegram API works

It's plain HTTP. Every call is `https://api.telegram.org/bot<TOKEN>/<method>`,
and every response is JSON shaped like:

```json
{ "ok": true, "result": [ ... ] }
```

You need three methods for the whole project: `getUpdates`, `sendMessage`, and
later `answerCallbackQuery`. That's it. **Don't reach for a library.** The
official Go bindings are semi-dormant, and wiring three endpoints yourself is
less code than learning someone's wrapper — plus you'll actually understand what
long polling is doing.

### The polling loop

`getUpdates` takes two parameters that matter:

- `timeout=30` — hold the connection open up to 30 seconds waiting for
  something. This is the "long" in long polling.
- `offset` — the acknowledgement mechanism. Telegram keeps redelivering an
  update until you confirm it. You confirm by passing
  `offset = highest_update_id_seen + 1` on your *next* call.

So the loop is: call `getUpdates` with your offset → get zero or more updates →
handle them → update your offset → repeat forever.

### Steps

1. Talk to [@BotFather](https://t.me/BotFather) on Telegram, `/newbot`, get a
   token. Put it in `.env` (already gitignored). Get your numeric user ID from
   [@userinfobot](https://t.me/userinfobot).

2. Create `internal/telegram/client.go`. Start with the transport, because every
   method shares it:

   ```go
   type Client struct {
       token string
       http  *http.Client
   }

   func New(token string) *Client {
       return &Client{
           token: token,
           // Must exceed the getUpdates timeout, or every poll fails.
           http: &http.Client{Timeout: 60 * time.Second},
       }
   }
   ```

3. Define the response types. Go decodes JSON into structs using **struct tags**,
   which you can't guess, so here's the shape:

   ```go
   type Update struct {
       UpdateID int64    `json:"update_id"`
       Message  *Message `json:"message"`
   }

   type Message struct {
       Text string `json:"text"`
       From *User  `json:"from"`
       Chat *Chat  `json:"chat"`
   }

   type User struct {
       ID int64 `json:"id"`
   }
   ```

   Note the pointers: `*Message` is nil when an update isn't a message. Go has no
   nullable value types, so a pointer *is* how you express "might be absent".

4. Write `GetUpdates(ctx context.Context, offset int64) ([]Update, error)` and
   `SendMessage(ctx context.Context, chatID int64, text string) error`.

5. Write the loop in `main.go`. **Echo the message back**, prefixed with
   something so you know it's yours.

6. **The allowlist.** Ignore any update where the sender isn't your configured
   ID. Get this right now, because from Phase 5 this bot writes to a public
   website.

### Watch out for

- **Check `from.id`, not `chat.id`.** They're identical in a private chat and
  divergent the moment the bot is added to a group.
- **Drain the backlog on startup.** Telegram holds undelivered updates for 24
  hours. Restart the bot after a week away and it will replay every message you
  ever sent it. On startup, call `getUpdates` with `offset=-1`, discard the
  result, and start from there.
- **Only ever run one instance.** Two processes polling the same bot gets you a
  `409 Conflict`. Remember this when your laptop copy is running and you deploy.

### Then deploy it

Yes, now — while it does nothing but echo. `GOOS=linux GOARCH=amd64 go build`,
`scp` the binary up, run it by hand with the env vars set.

This feels premature. It isn't. You want the VPS, the token handling, and the
network path debugged while the only logic is "say hello", not while you're
also chasing a YAML bug. Do the proper systemd unit in Phase 8.

**Done when:** you message the bot from your phone, it echoes; a friend messages
it, nothing happens; and it does both from the VPS.

---

## Phase 3 — The frontmatter editor

**Goal:** the heart of the project, and the best Go you'll write. No network, no
Telegram — pure functions and tests.

### The core insight

You need to change `status: reading` to `status: finished` in an existing file
without disturbing anything else. The obvious approach is: parse the YAML into a
map, change the value, re-emit it.

**Don't.** Here's what happens if you do.

Your content folder is also an Obsidian vault you hand-edit. Obsidian won't
quote dates for you, so sooner or later a note has `created: 2026-07-25` without
quotes. Go's YAML library resolves that to a `time.Time`, and re-emitting turns
it into `created: 2026-07-25T00:00:00Z` — which, per the contract above, means
that page now silently displays your build date. **The bot breaks its own rule,
on a file it didn't write.**

Round-tripping also guarantees cosmetic damage: Go maps have no order so your
keys get shuffled, `link:` (empty) becomes `link: null`, indentation changes,
and any `summary` with an apostrophe gets re-quoted. Every commit looks like the
bot is fighting your vault.

So, the rule:

> **Parse the YAML to read it. Edit lines to write it.**

Decode freely to find out what `status` currently is — then throw that away and
change the file with a targeted line replacement. Everything you didn't touch
survives byte-for-byte, by construction. You only ever modify three or four
keys, so this is *less* code than the round trip, not more.

### Steps

1. `internal/note/note.go`. Split a file into frontmatter and body:

   ```go
   func Split(raw []byte) (frontmatter, body []byte, err error)
   ```

   The frontmatter is between the first `---` line and the next `---` line. A
   file without them is an error.

2. Parse for reading. Add `gopkg.in/yaml.v3` (`go get gopkg.in/yaml.v3`) and
   decode into a struct with `yaml:"..."` tags — same idea as the JSON tags.
   You need `title`, `status`, `created`. **Decode `created` as a `string`**, not
   a `time.Time`, so a malformed one is visible rather than silently coerced.

3. `func SetField(raw []byte, key, value string) ([]byte, error)` — the line
   editor. Walk the frontmatter lines; if one starts with `key:`, replace it; if
   none does, insert before the closing `---`. Return the whole file.

4. `func AppendToLog(raw []byte, date, text string) ([]byte, error)` — find a
   `## Log` heading and append `- **2026-07-25** — text` under it, creating the
   section at the end of the file if it's absent. An anchored section keeps the
   file hand-editable; appending blindly to the end lands your note after
   `_Still reading._`, which reads wrong.

5. `func Slug(title string) string` — `The Handmaid's Tale` → `the-handmaids-tale`.
   Lowercase, strip punctuation, spaces to hyphens, collapse runs.

### Test it properly — this is the Go lesson

Go's testing has no assertion library by default and doesn't want one. The
idiom is **table-driven tests**:

```go
func TestSlug(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  string
    }{
        {"apostrophe", "The Handmaid's Tale", "the-handmaids-tale"},
        {"colon", "Dune: Part Two", "dune-part-two"},
        {"leading article kept", "A Wizard of Earthsea", "a-wizard-of-earthsea"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := Slug(tt.input); got != tt.want {
                t.Errorf("Slug(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

Add a row, get a case. `t.Run` gives each its own name in the output.

**The test that matters most**, and the one to write first:

```go
// A file with an UNQUOTED created date must survive a status change
// with that line untouched. This is the bug that made line-editing the
// design; if this test passes, the whole approach is validated.
func TestSetFieldPreservesUnquotedDate(t *testing.T) { ... }
```

### Fixtures must be synthetic

Put sample markdown in `internal/note/testdata/`. **Invent the books.** Your
content repo is private and some notes are marked `private: true` — deliberately
unpublished — while *this* repo is public. Copying real notes in to use as
fixtures publishes them, and it won't feel like a mistake at the time because
it's "just markdown". The code cares about YAML and line offsets, not whether
the prose is real.

**Done when:** you can round-trip a file through `SetField` and `git diff` shows
exactly one changed line.

---

## Phase 4 — The GitHub client

**Goal:** read and write files over HTTP.

Three endpoints:

- `GET /repos/{owner}/{repo}/contents/{path}?ref={branch}` — a file. Returns
  base64 `content` and a `sha`.
- `GET` the same on a *directory* — returns an array of names and SHAs, no
  content.
- `PUT /repos/{owner}/{repo}/contents/{path}` — write. Body takes `message`,
  base64 `content`, `branch`, and `sha`.

That `sha` is the whole concurrency story. You send back the SHA of the blob you
read; if someone else changed the file since, GitHub rejects you with `409`
rather than silently clobbering. This matters because Obsidian is pushing to the
same repo. Handle the 409: re-read, re-apply, retry once, then give up and tell
the user.

Two behaviours worth knowing because they turn errors into features:

- **PUT with no `sha` = create**, and it fails `422` if the file already exists.
  That's your "does this book exist?" check, free.
- Always pass `branch` explicitly, even when it matches the default.

### Steps

1. `internal/github/client.go` with `GetFile`, `ListDir`, `PutFile`.
2. Authenticate with `Authorization: Bearer <token>`.
3. Remember to base64-decode on read and encode on write (`encoding/base64`).
4. On any `401`, send yourself a Telegram message. Fine-grained PATs expire, and
   a bot that silently stops working for a fortnight is worse than one that
   complains.

**Done when:** a throwaway `main` can read a file from the repo and print it.

---

## Phase 5 — Create a note, end to end

**Goal:** the first real write. Also the phase where Go's interfaces click.

### The lesson: define interfaces where you consume them

You don't want tests that hit GitHub. In C# you'd extract an `IGitHubClient`,
put it next to the implementation, and inject it. In Go you declare the
interface **in the package that uses it**, listing only the methods that package
actually needs:

```go
// in internal/bot — not in internal/github
type contentStore interface {
    GetFile(ctx context.Context, path string) ([]byte, string, error)
    PutFile(ctx context.Context, path string, content []byte, sha, msg string) error
}
```

`*github.Client` now satisfies this without knowing it exists — no `implements`,
no declaration, no reference from `github` to `bot`. Write a `fakeStore` in your
test file that records what it was asked to write, and you can assert on exact
file content with no network at all.

This is the single biggest idiomatic difference from C#, and this is the moment
to internalise it.

### Steps

1. Build the note content: `title`, quoted `created` (today), `status: reading`,
   `author`, `summary`, `draft: false`, tags.

2. **Dates.** Go's time formatting uses a reference date rather than
   `yyyy-MM-dd`. It's `time.Now().Format("2006-01-02")` — those exact digits,
   which are the reference time `01/02 03:04:05PM '06 -0700`. It looks
   arbitrary because it is.

   Set the timezone explicitly: `time.LoadLocation("Europe/London")`. Your VPS
   is UTC, so a book you start at 23:30 in summer gets tomorrow's date. And add
   `import _ "time/tzdata"` — that underscore import embeds the timezone
   database in the binary, because a minimal VPS image may not have one. (An
   underscore import means "run this package's initialisation, I don't need its
   name" — you'll see it used for driver registration too.)

3. Wire up `DRY_RUN`: when on, reply with the exact file content instead of
   calling `PutFile`. Use it until you trust the output.

4. Turn it off. Send yourself a title. Watch the Actions run and the page appear.

**Done when:** a real book page is live, under the right heading on the index,
showing the right date.

### Deliberately not yet

You haven't touched Open Library. That's on purpose — writing correct
frontmatter to a public site is the risky, irreversible part; metadata lookup is
a convenience. Get the dangerous thing right while the inputs are hardcoded.

---

## Phase 6 — Metadata lookup and buttons

**Goal:** make it pleasant. Conversation state, inline keyboards.

### Open Library

No API key: `https://openlibrary.org/search.json?title=<title>&fields=title,author_name,first_publish_year,cover_i&limit=5`

Use `title=` rather than `q=`; the ordering is noticeably better. `author_name`
is an array. Show the top three as buttons.

**Never let this block a create.** Open Library is bad at obscure books, and
being unable to add a book because a free API shrugged is infuriating. Always
offer "none of these — use what I typed".

### Inline keyboards

Attach `reply_markup` to `sendMessage`:

```json
{"inline_keyboard": [[{"text": "Mark finished", "callback_data": "abc123"}]]}
```

Tapping sends you an `Update` with a `CallbackQuery` instead of a `Message`.
Three things:

- **`callback_data` is capped at 64 bytes.** Don't encode an action plus a slug
  in there — `the-brief-wondrous-life-of-oscar-wao` alone nearly fills it. Store
  the pending action in an in-memory map keyed by a short random ID and put
  *that* in the button.
- **You must call `answerCallbackQuery`**, or the user's button spins until it
  times out. Even with no text.
- **Allowlist the callback path too.** It's `update.CallbackQuery.From.ID`, a
  different field from the message path. Guarding messages and forgetting
  callbacks is easy — and callbacks are what actually write to your site.

### State and restarts

In-memory state means a restart drops pending confirmations. Don't fight it:
when a callback ID isn't in the map, reply "that expired, send the title again".
A clear message beats a mysterious silence, and it's three lines.

**Done when:** you send "testaments", pick from a keyboard, and a page appears.

---

## Phase 7 — Find and update an existing book

**Goal:** fuzzy matching, and knowing when to stop.

You'll type "testaments", not "The Testaments".

**Don't build a title index.** `ListDir` on `content/reading` gives you every
filename in one call — always fresh, nothing to invalidate. The filenames *are*
slugs, so match against those, then fetch only the one file the user picks.

Start with `strings.Contains` on the slug. Genuinely — try it for a week. You
have maybe a hundred books and you know their titles; substring matching will be
right almost always. Add Levenshtein only when you can name a real case where it
failed. Reaching for a fuzzy-matching library on day one is the classic version
of this mistake.

Handle: no match (offer to create), one match (go straight to actions), several
(disambiguation keyboard).

### What an update writes

- **Status** → a frontmatter edit via `SetField`.
- **Finishing** → also write `finished: "YYYY-MM-DD"`. Cheap now,
  unreconstructable later, and it's the most interesting field in a reading log
  a year from now.
- **Rating** → `rating: 8` in frontmatter, *not* buried in the summary prose.
  Quartz ignores frontmatter it doesn't recognise, so this costs nothing today
  and you can surface it in the template later without touching the bot.
  Structured data belongs in structured fields.
- **Progress note** → `AppendToLog`.

**Done when:** "testaments" → tap *finished* → tap *8/10* → the index moves it
under the right heading.

---

## Phase 8 — Deploy properly

**Goal:** it survives a reboot.

1. **Build for the VPS:** `GOOS=linux GOARCH=amd64 go build -o reading-bot ./cmd/reading-bot`.
   No runtime, no dependencies — this is why Go suits this job. `scp` it up.

2. **Secrets go in an EnvironmentFile**, mode `0600`, owned by the service user —
   *not* inline in the unit file, which is world-readable.

3. **A systemd unit** with `Restart=always`, `RestartSec=10`, and the hardening
   basics: `DynamicUser=yes`, `NoNewPrivileges=yes`, `ProtectSystem=strict`,
   `PrivateTmp=yes`. Read `man systemd.exec` for what each does rather than
   pasting blind — a chunk of this project's value is being able to explain it.

4. **Graceful shutdown.** This is where `context` earns its place. Catch
   `SIGTERM` with `signal.NotifyContext`, cancel the context, let the poll loop
   exit cleanly. Otherwise systemd kills you mid-write.

5. **Logs:** `journalctl -u reading-bot -f`. Log slugs, never note bodies — the
   failure mode is pasting a log into a public issue and shipping a page's
   contents with it.

**Done when:** `systemctl restart` and a VPS reboot both leave you with a
working bot.

---

## Phase 9 — Polish

Only once it's been used for real, in rough priority order:

- **Covers.** Store `cover: https://covers.openlibrary.org/b/id/<id>-L.jpg` in
  frontmatter. Don't download the image — the Contents API writes one file per
  call, so the note and the image can't be one commit, and you've added binaries
  to your content repo for nothing.
- **Batch edits.** Right now three taps is three commits and three deploys.
  Stage changes in memory and commit once on "done".
- **`obsidian-git` friction.** The bot pushes, so your vault is behind until it
  pulls. Set the plugin to auto-pull on an interval.
- **A `/status` command** listing what you're currently reading. Read-only,
  quick, and surprisingly useful.

---

## How to use AI on this without it doing it for you

You'll be tempted to paste a phase into a chat and get working code. That
produces a bot and no learning. Some things that work better:

**Ask for the concept, not the code.** "Explain Go's pointer vs value receivers
using my `loader` struct as the example" teaches you something. "Write
SetField for me" doesn't.

**Write it badly first, then ask for a review.** Attempt it, get it compiling,
then ask "here's my `AppendToLog` — what would a Go reviewer say?" You'll learn
more from having your own code critiqued than from reading someone else's.

**Ask "why doesn't this compile?" and make it explain**, rather than asking for
a fix. Go's errors around pointers, interfaces and nil are unfamiliar but
consistent; understanding one teaches you a category.

**Get it to write tests against a signature, then make them pass.** Genuinely
good division of labour: the test encodes the requirement, you write the
implementation. That's what Phase 3 is set up for.

**Ask it to argue with you.** "I'm going to store the rating in the summary
field — what breaks in six months?" A good answer changes your design; a bad one
you'll spot, which is its own kind of progress.

The tell that you've gone too far: you can't explain a file you're about to
commit. Stop and go back a step.

---

## Appendix: getting unstuck in Go

- **`go doc <pkg> <symbol>`** — offline docs. `go doc strings Builder`.
- **[pkg.go.dev](https://pkg.go.dev)** — the standard library, well indexed.
  You'll need `net/http`, `encoding/json`, `strings`, `time`, `context`.
- **[Go by Example](https://gobyexample.com)** — short runnable snippets. The
  fastest way to remember syntax you've half-forgotten.
- **`gofmt`** — never argue about formatting. There's one style.
- **Compile errors mentioning `nil pointer dereference`** — you dereferenced an
  optional field. In this project it's almost always `update.Message` on an
  update that was actually a callback.
- **`declared and not used`** — Go refuses to compile unused local variables.
  This is deliberate and you'll come to like it.
