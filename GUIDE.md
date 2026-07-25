# Building a Telegram reading bot in Go: a learning guide

This is a build-it-yourself guide for a small, real project: a Telegram bot you
message with reading updates, which commits directly to the markdown repo behind
a [Quartz](https://quartz.jzhao.xyz/) site. Tell it "started The Testaments" and
a published page appears a minute later.

It assumes you can program but have never written Go and have never built a
Telegram bot. It's written for someone coming from C#/.NET, so there are
"coming from .NET" notes where Go's habits differ sharply.

**The guide does not contain the finished code.** Each phase tells you what to
learn, what to build, and how to know it works. Where a Go idiom is unguessable
(struct tags, the time format, the HTTP client pattern) it's shown outright,
because memorising trivia isn't the lesson. Where the design decision *is* the
lesson, you get a signature and a failing test.

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
A webhook means Telegram makes an inbound HTTP request to you, which needs a
public IP, an open port, a TLS certificate, a domain name, and a reverse proxy.
Long polling means *you* call Telegram's `getUpdates` and it holds the
connection open until something happens. Only outbound connections. The
deployment collapses to one binary and one systemd unit. The cost is latency
measured in a second or two, which for a personal bot is nothing.

**The GitHub Contents API, not a git clone on the server.** The obvious approach
is to clone the repo onto the VPS and `git commit && git push`. Don't. That puts
an SSH key on the box, requires the disk to stay in sync, and means the bot must
eventually resolve a merge conflict at 11pm, unsupervised, against your website.
Instead, the GitHub REST API lets you read a file (getting its blob SHA) and
write it back in one call, committing server-side. The VPS holds no state. The
cost: one edit is one commit is one deploy, and you can't write two files
atomically.

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
  `The Handmaid's Tale` becomes `the-handmaids-tale.md`.

### Before you start

You need a Go module and a few housekeeping files in place. If you're starting
from nothing, create them now (each is a few lines, and none of it is the
lesson):

- `go.mod`, from `go mod init github.com/<you>/reading-bot`
- `.gitignore` containing at minimum `.env`, `.idea/`, and your built binary
- `.env.example` listing every variable with empty values, committed as
  documentation
- `.env` with the real values, never committed

Everything else in the guide, including the config loader, you build yourself.

### How long this takes, honestly

Estimates assume you're new to Go and working evenings.

| Phase | | Evenings |
|---|---|---|
| 0 | Enough Go to start | 2 to 3 |
| 1 | The config loader | 1 |
| 2 | A bot that talks to you | 2 |
| 3 | The frontmatter editor | 2 to 3 |
| 4 | The GitHub client | 2 |
| 5 | Create a note, end to end | 1 to 2 |
| 6 | Knowing when you've broken the site | 1 to 2 |
| 7 | Metadata lookup and buttons | 2 |
| 8 | Find and update an existing book | 2 |
| 9 | Deploy properly | 1 |
| 10 | Polish | open-ended |

**Phases 0 to 6 are the project.** At the end of Phase 6 you have a bot you use
daily: it creates real pages, it tells you when it has broken something, and it
can undo itself. Every Go concept the project uses has appeared by then.

**Phases 7 to 10 add capability but teach little that is new.** They're worth
doing, and they mostly recombine pieces you already wrote. If you run out of
momentum at Phase 6, you have a finished thing, not an abandoned one. That is a
deliberate property of this ordering.

---

## Phase 0: enough Go to start

**Goal:** not fluency. Enough to write the config loader without guessing.

Work through [A Tour of Go](https://go.dev/tour/), but only these:

- **Basics:** packages, imports, functions, variables, `:=`, zero values.
- **Methods and interfaces:** stop after "Stringers". This is the section that
  matters most and the one that will feel strangest.
- **Errors:** short, essential.

**Skip for now:** goroutines, channels, generics. You'll write exactly one
goroutine in this project (Phase 6) and Phase 7 explains why you don't need any
of the machinery that usually comes with them.

### Coming from .NET

| C# | Go |
|---|---|
| `public` / `private` | Capitalised name is exported, lowercase isn't. It's the *identifier*, not a keyword. |
| `throw` / `try-catch` | No exceptions. Functions return `(value, error)` and you check it every time. |
| `class Foo : IBar` | Interfaces are satisfied **implicitly**. A type never declares that it implements one. |
| `null` | Zero values. An unset `string` is `""`, an `int` is `0`, a struct is fully-formed with zeroed fields. |
| LINQ | Nothing like it. You write the loop. This is fine. |
| `async`/`await` | Not needed here. Go's I/O is blocking, and concurrency is goroutines. |

The two that will trip you most: **errors are values you must handle explicitly**
(Go's `if err != nil` is famously verbose and it's the correct trade), and
**implicit interfaces**, where you define an interface at the point it's
*consumed*, not where it's implemented. That's backwards from C# and it's what
makes Go easy to test, as you'll see in Phase 5.

### Tooling

```bash
go run ./cmd/reading-bot     # compile and run
go test ./...                # run every test
go test -race ./...          # run tests with the data race detector
go vet ./...                 # catch likely mistakes
gofmt -w .                   # format (there is one style, don't argue with it)
go doc net/http Client       # read docs offline
```

Run these from the terminal at least a few times even if you use an IDE. When
something breaks on the VPS, the terminal is all you'll have.

### If you're using GoLand

It'll feel like Rider, because it is. Four things worth setting up now:

- **A run configuration with environment variables.** Run, then Edit
  Configurations, then Go Build, point it at `./cmd/reading-bot`, and put your
  variables in the *Environment* field. **This is a secrets hazard:** GoLand
  stores run configurations under `.idea/`, so your bot token ends up in a file
  in the repo. Make sure `.idea/` is gitignored. Better still, install the
  **EnvFile** plugin and point it at `.env`, so the token lives in one
  gitignored place instead of two.
- **Green arrows in the test gutter** run a single test, and `t.Run` subtests
  get their own arrow. This makes the table-driven tests in Phase 3 genuinely
  pleasant: add a row, run just that row.
- **Format on save.** Settings, then Tools, then Actions on Save, then tick
  *Reformat code* and *Optimize imports*. Go has exactly one correct format and
  GoLand knows it, so stop thinking about it permanently.
- **The debugger works properly:** breakpoints, stepping, variable inspection,
  same as Rider. Worth using in Phase 2 to look at a real `Update` struct and
  see which fields are nil. That's a faster way to understand Telegram's shape
  than reading their docs.

Two habits from .NET that don't carry over: you won't need to configure a build
system (`go build` is the whole thing), and GoLand's "implement interface"
tooling matters much less here, because Go interfaces are satisfied implicitly
and there's nothing to generate.

**Done when:** you can explain what `func (c *Config) Redacted() string` would
mean, every part of it, including why there's a `*`.

---

## Phase 1: the config loader

**Goal:** your first real Go package, and a habit that pays off at deploy time.

Everything the bot needs comes from environment variables. No config file in the
repo means no config file to accidentally commit, and it means the same binary
runs on your laptop and the VPS with nothing but a different environment.

### The spec

Build `internal/config` exposing `Load() (*Config, error)`, reading:

| Variable | Required | Default |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | yes | |
| `TELEGRAM_ALLOWED_USER_ID` | yes, must parse as a positive integer | |
| `GITHUB_TOKEN` | yes | |
| `GITHUB_OWNER` | yes | |
| `GITHUB_REPO` | yes | |
| `GITHUB_BRANCH` | no | `v4` |
| `CONTENT_DIR` | no | `content/reading` |
| `DRY_RUN` | no | `true` |

Four requirements that are the actual exercise:

1. **Report every problem at once**, not just the first. Collect them in a
   slice and return one error listing all of them.
2. **`Redacted() string`** returns a line safe to write to a log: enough to tell
   two tokens apart, not enough to use one.
3. **`DRY_RUN` defaults to on**, and an unparseable value (`DRY_RUN=yes`) is an
   *error*, not `false`.
4. **Trim leading and trailing slashes from `CONTENT_DIR`.** A stray slash
   produces a double slash in a GitHub API path, which 404s in a way that looks
   exactly like a missing file.

Write it, then write `main.go` to load the config, log the redacted line, and
exit non-zero on failure, so systemd records a failed start rather than a
process that looks alive and can do nothing.

### Test it

You'll want `t.Setenv`, which sets a variable for one test and restores it
afterwards. One trap: it only restores what *it* set, so a variable already in
your shell leaks into the test. Clear every key you read at the start of each
test, or your tests will pass for you and fail in CI.

Cases worth having: all defaults applied, every missing variable named in one
error, a `@username` rejected as a user ID, `DRY_RUN=yes` rejected,
`CONTENT_DIR=/content/reading/` trimmed, and `Redacted()` not containing either
token in full.

### Self-review

When it passes, answer these out loud. If you can't, you copied something.

1. Why put the package under `internal/`? (Search "go internal packages". It's
   a compiler-enforced visibility rule, and `go doc` won't tell you.)
2. Your accumulator method: is the receiver a pointer or a value? What breaks if
   you get it wrong? (This one catches almost everybody once.)
3. Why is reporting all problems at once better than failing on the first, for
   something running under systemd?
4. Argue in one sentence for treating `DRY_RUN=yes` as an error.
5. Why must the test clear every key before setting any?

Then break it deliberately: change the `DRY_RUN` default to `false`, run
`go test ./...`, read the failure, change it back. Getting comfortable reading
Go test output now saves you an hour later.

**Done when:** running with an empty environment prints all five missing
variables and exits 1.

---

## Phase 2: a bot that talks to you and nobody else

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
less code than learning someone's wrapper, plus you'll actually understand what
long polling is doing.

### The polling loop

`getUpdates` takes two parameters that matter:

- `timeout=30`: hold the connection open up to 30 seconds waiting for something.
  This is the "long" in long polling.
- `offset`: the acknowledgement mechanism. Telegram keeps redelivering an update
  until you confirm it. You confirm by passing
  `offset = highest_update_id_seen + 1` on your *next* call.

So the loop is: call `getUpdates` with your offset, get zero or more updates,
handle them, update your offset, repeat forever.

### Steps

1. Talk to [@BotFather](https://t.me/BotFather) on Telegram, `/newbot`, get a
   token. Put it in `.env`. Get your numeric user ID from
   [@userinfobot](https://t.me/userinfobot).

2. Create `internal/telegram/client.go`. Start with the transport, because every
   method shares it:

   ```go
   type Client struct {
       baseURL string
       token   string
       http    *http.Client
   }

   func New(token string) *Client {
       return &Client{
           baseURL: "https://api.telegram.org",
           token:   token,
           // Must exceed the getUpdates timeout, or every poll fails.
           http: &http.Client{Timeout: 60 * time.Second},
       }
   }
   ```

   `baseURL` is a field rather than a constant so a test can point the client at
   a local server. Phase 4 shows how, and you'll come back and use it here.

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

### Staying up when the network doesn't

Your first version of the loop probably returns an error upward when
`getUpdates` fails, which kills the process. That's wrong. A transient failure
is normal and expected: your VPS's network blips, Telegram returns a 502 during
a deploy, or you get rate limited. None of those should stop the bot.

The rule: **the poll loop logs and continues. It never returns a transient
error upward.** The only reason to exit the loop is a cancelled context, which
means you're shutting down deliberately.

But "continue" alone gives you a hot loop. If Telegram is down, an immediate
retry hammers it thousands of times a minute, burns your VPS's CPU, and fills
the journal. So: **exponential backoff with a cap.**

- Start at 1 second. Double after each consecutive failure. Cap at 60 seconds.
- **Reset to zero on any success.** Forget this and one blip permanently slows
  the bot to a 60 second poll until you restart it.
- The cap is the important half. Unbounded doubling means an overnight outage
  leaves you waiting hours to notice recovery.

Two specifics:

- **Distinguish shutdown from failure.** If the error is because your context
  was cancelled, return cleanly. Don't back off and retry your way through a
  `SIGTERM`.
- **Honour `retry_after` on a 429.** Telegram tells you exactly how long to wait:
  `{"ok":false,"error_code":429,"parameters":{"retry_after":30}}`. If it's
  present, use it in preference to your own backoff. Guessing when you've been
  told the answer is how you get rate limited harder.

One thing that is *not* a failure: a long poll that returns
`{"ok":true,"result":[]}` after 30 seconds. That's the normal quiet case.
Only an actual transport error or a non-`ok` response counts.

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

Yes, now, while it does nothing but echo. `GOOS=linux GOARCH=amd64 go build`,
`scp` the binary up, run it by hand with the env vars set.

This feels premature. It isn't. You want the VPS, the token handling, and the
network path debugged while the only logic is "say hello", not while you're also
chasing a YAML bug. Do the proper systemd unit in Phase 9.

**Done when:** you message the bot from your phone and it echoes, a friend
messages it and nothing happens, it does both from the VPS, and pulling the
network cable makes it back off and recover rather than die.

---

## Phase 3: the frontmatter editor

**Goal:** the heart of the project, and the best Go you'll write. No network, no
Telegram, just pure functions and tests.

### The core insight

You need to change `status: reading` to `status: finished` in an existing file
without disturbing anything else. The obvious approach is: parse the YAML into a
map, change the value, re-emit it.

**Don't.** Here's what happens if you do.

Your content folder is also an Obsidian vault you hand-edit. Obsidian won't
quote dates for you, so sooner or later a note has `created: 2026-07-25` without
quotes. Go's YAML library resolves that to a `time.Time`, and re-emitting turns
it into `created: 2026-07-25T00:00:00Z`, which per the contract above means that
page now silently displays your build date. **The bot breaks its own rule, on a
file it didn't write.**

Round-tripping also guarantees cosmetic damage: Go maps have no order so your
keys get shuffled, `link:` (empty) becomes `link: null`, indentation changes,
and any `summary` with an apostrophe gets re-quoted. Every commit looks like the
bot is fighting your vault.

So, the rule:

> **Parse the YAML to read it. Edit lines to write it.**

Decode freely to find out what `status` currently is, then throw that away and
change the file with a targeted line replacement. Everything you didn't touch
survives byte-for-byte, by construction. You only ever modify three or four
keys, so this is *less* code than the round trip, not more.

There is a second reason for this rule, which you'll meet in Phase 4 when two
writers collide. It's the more interesting one.

### Steps

1. `internal/note/note.go`. Split a file into frontmatter and body:

   ```go
   func Split(raw []byte) (frontmatter, body []byte, err error)
   ```

   The frontmatter is between the first `---` line and the next `---` line. A
   file without them is an error.

2. Parse for reading. Add `gopkg.in/yaml.v3` (`go get gopkg.in/yaml.v3`) and
   decode into a struct with `yaml:"..."` tags, the same idea as the JSON tags.
   You need `title`, `status`, `created`. **Decode `created` as a `string`**,
   not a `time.Time`, so a malformed one is visible rather than silently
   coerced.

3. `func SetField(raw []byte, key, value string) ([]byte, error)`, the line
   editor. Walk the frontmatter lines. If one starts with `key:`, replace it. If
   none does, insert before the closing `---`. Return the whole file.

4. `func AppendToLog(raw []byte, date, text string) ([]byte, error)`. Find a
   `## Log` heading and append `- **2026-07-25**: text` under it, creating the
   section at the end of the file if it's absent. An anchored section keeps the
   file hand-editable. Appending blindly to the end lands your note after
   `_Still reading._`, which reads wrong.

5. `func Slug(title string) string`. See the rules below, because this is
   fiddlier than it looks.

### Test it properly, this is the Go lesson

Go's testing has no assertion library by default and doesn't want one. The idiom
is **table-driven tests**:

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

### Slug rules, including the ones that bite

The table above is all ASCII, which is why it lulls you. Real book titles are
not. Decide the rule first, then write the tests, then implement:

1. Lowercase everything.
2. `&` becomes the word `and`. (`Crime & Punishment` reads badly as
   `crime-punishment`.)
3. **Transliterate accented Latin characters to ASCII:** `é` becomes `e`, `ø`
   becomes `o`. Don't strip them, or `Les Misérables` becomes `les-misrables`.
4. Strip anything still left that isn't `a-z`, `0-9`, a space or a hyphen.
5. Collapse runs of spaces and hyphens into a single hyphen, then trim from both
   ends.
6. **If the result is empty, fail loudly.** Don't guess.

Rows worth adding:

| Input | Expected |
|---|---|
| `Les Misérables` | `les-miserables` |
| `Crime & Punishment` | `crime-and-punishment` |
| `Stalingrad: The Fateful Siege` | `stalingrad-the-fateful-siege` |
| `1984` | `1984` |
| `  Molloy  ` | `molloy` |
| `こころ` | (empty, must be rejected) |

That last one is why rule 6 exists. A title in a script your transliterator
doesn't handle produces an empty slug, and an empty slug means writing to
`content/reading/.md`: a hidden file, invisible in Obsidian, invisible on the
site, and confusing for a long time. Return an error and have the bot ask you to
supply a filename.

For step 3, the idiomatic approach uses `golang.org/x/text`: normalise to NFD,
which decomposes `é` into `e` plus a combining accent, then drop everything in
the `unicode.Mn` (nonspacing mark) category. Look up `norm.NFD`,
`transform.Chain` and `runes.Remove`. The alternative is a hand-rolled map of
thirty-odd characters, which has no dependency and will miss something. For a
reading list that will eventually contain a Scandinavian crime novel, take the
dependency.

### Fixtures must be synthetic

Put sample markdown in `internal/note/testdata/`. **Invent the books.** Your
content repo is private and some notes are marked `private: true`, deliberately
unpublished, while the code repo is public. Copying real notes in to use as
fixtures publishes them, and it won't feel like a mistake at the time because
it's "just markdown". The code cares about YAML and line offsets, not whether
the prose is real.

**Done when:** you can round-trip a file through `SetField` and `git diff` shows
exactly one changed line, and `Slug` handles every row above.

---

## Phase 4: the GitHub client

**Goal:** read and write files over HTTP, and handle the one genuinely tricky
correctness problem in the project.

Three endpoints:

- `GET /repos/{owner}/{repo}/contents/{path}?ref={branch}`: a file. Returns
  base64 `content` and a `sha`.
- `GET` the same on a *directory*: returns an array of names and SHAs, no
  content.
- `PUT /repos/{owner}/{repo}/contents/{path}`: write. Body takes `message`,
  base64 `content`, `branch`, and `sha`.

Two behaviours worth knowing because they turn errors into features:

- **PUT with no `sha` means create**, and it fails `422` if the file already
  exists. That's your "does this book exist?" check, free.
- Always pass `branch` explicitly, even when it matches the default.

### Steps

1. `internal/github/client.go` with `GetFile`, `ListDir`, `PutFile`. Give it a
   `baseURL` field, as with the Telegram client.
2. Authenticate with `Authorization: Bearer <token>`.
3. Remember to base64-decode on read and encode on write (`encoding/base64`).
4. On any `401`, send yourself a Telegram message. Fine-grained PATs expire, and
   a bot that silently stops working for a fortnight is worse than one that
   complains.

### Conflicts, and why they're the interesting part

That `sha` in the PUT body is optimistic concurrency control. You send back the
SHA of the blob you read. If anyone changed the file since you read it, GitHub
rejects you with `409 Conflict` rather than silently overwriting their work.

This is not theoretical. Your content repo is also an Obsidian vault with
`obsidian-git` pushing to it. You edit a note on your laptop, the plugin pushes,
and thirty seconds later you tell the bot to mark that book finished. The bot is
holding a SHA that is now stale. This is the only place in the project where two
writers genuinely collide, and it's worth getting exactly right.

**The wrong fix, which looks right.** Catch the 409, re-fetch just to get the
current SHA, and re-send the bytes you already built. This appears to work. It
also silently destroys the edit you made in Obsidian: your bytes were computed
from the *old* content, so writing them discards everything that changed in
between. GitHub told you there was a conflict and you resolved it by ignoring
the other party. This is the classic lost update, and it's worse than crashing,
because nothing tells you it happened.

**The right fix.** Re-apply the *operation*, not the *bytes*:

1. `GetFile` again. You now have fresh content and a fresh SHA.
2. Run `SetField` against that **newly fetched content**.
3. `PutFile` with the new SHA.

The distinction is the whole lesson. Your change is not "this file should
contain these 900 bytes". Your change is "set `status` to `finished`". An
operation can be applied to content you haven't seen before and still do the
right thing. A snapshot cannot.

**And this is the second reason for line editing.** Re-applying an operation is
only safe because `SetField` touches exactly one line and leaves the rest of the
file untouched, including parts you don't understand. A YAML round trip
regenerates the *entire file* from a parsed model, so anything your model
doesn't represent (key order, comments, fields you didn't declare in your
struct, the exact bytes of the body) gets rebuilt from your assumptions rather
than preserved. Retrying a round trip would report success and still clobber the
other writer's changes. The conflict handler would be a data loss bug wearing a
fix's clothing.

Line editing makes the retry trivially correct. That's not a coincidence, it's
the payoff.

**The details:**

- **Retry limit: three attempts total** (the first, plus two retries). Two
  genuine collisions in a row is already unlikely. Three means something is
  persistently wrong and looping won't help.
- **Pause briefly between attempts**, a few hundred milliseconds. An
  `obsidian-git` push takes a moment to land, and retrying instantly means
  racing the same in-flight push again.
- **Check before you write.** After re-fetching, if the content already has the
  value you were going to set, someone (or a previous partly-failed attempt)
  already did it. Report success and skip the write. This makes the whole
  operation idempotent and avoids an empty commit.
- **When you give up, be specific.** Not "something went wrong". Say what
  failed, that nothing was saved, and what to do:

  > Couldn't mark *the-testaments* as finished: the file kept changing while I
  > was writing it (3 attempts). Nothing was saved. Try again in a moment.

  Naming the file matters, and "nothing was saved" matters more. The worst
  failure message is one that leaves you unsure whether to retry.

### Testing an HTTP client, without the internet

You don't want tests that hit GitHub. In .NET you'd mock `HttpMessageHandler` or
reach for a library. Go's answer is different and more direct: **spin up a real
HTTP server in the test.**

```go
func TestGetFileDecodesContent(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(
        func(w http.ResponseWriter, r *http.Request) {
            // Assert on the request as well as canning the response.
            if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
                t.Errorf("Authorization = %q, want Bearer test-token", got)
            }
            w.Header().Set("Content-Type", "application/json")
            // "aGVsbG8=" is base64 for "hello".
            fmt.Fprint(w, `{"content":"aGVsbG8=","sha":"abc123"}`)
        }))
    defer srv.Close()

    c := New("test-token")
    c.baseURL = srv.URL   // this is why baseURL is a field
    ...
}
```

`httptest.NewServer` starts a real server on a random local port and gives you
its URL. Your client makes a real HTTP request to it. That's the point: you're
testing your URL construction, your headers, your JSON decoding and your base64
handling, all of which a mocked interface would skip entirely.

Cases worth writing, and this is where the phase's value is:

- Happy path: content decoded, SHA extracted.
- **409:** does your retry re-fetch, or does it re-send stale bytes? Make the
  handler return `409` on the first PUT and `200` on the second, and assert that
  the second PUT's body was rebuilt from the *second* GET's content. This is the
  test that proves the section above.
- **422** on create: reported as "already exists", not as a generic failure.
- **401:** triggers your alert path.
- **Malformed JSON:** returns an error rather than panicking.

Then go back to Phase 2's Telegram client and do the same for `GetUpdates`,
including a `429` with `retry_after` so you can prove your backoff honours it.

**Done when:** `go test ./internal/github/...` covers the 409 retry, and a
throwaway `main` can read a real file from the repo and print it.

---

## Phase 5: create a note, end to end

**Goal:** the first real write. Also the phase where Go's interfaces click.

### The lesson: define interfaces where you consume them

You don't want your bot logic reaching for the network in tests. In C# you'd
extract an `IGitHubClient`, put it next to the implementation, and inject it. In
Go you declare the interface **in the package that uses it**, listing only the
methods that package actually needs:

```go
// in internal/bot, NOT in internal/github
type contentStore interface {
    GetFile(ctx context.Context, path string) ([]byte, string, error)
    PutFile(ctx context.Context, path string, content []byte, sha, msg string) error
}
```

`*github.Client` now satisfies this without knowing it exists: no `implements`,
no declaration, no reference from `github` to `bot`. Write a `fakeStore` in your
test file that records what it was asked to write, and you can assert on exact
file content with no network at all.

Note that this is a different tool from Phase 4's `httptest`, and both are
right. `httptest` tests *the client* against a real HTTP conversation. A fake
satisfying a small interface tests *the logic that uses the client*, without
caring how HTTP works. Using the fake for the client's own tests would prove
nothing, and using `httptest` for your bot logic would be slow and awkward.

This is the single biggest idiomatic difference from C#, and this is the moment
to internalise it.

### Steps

1. Build the note content: `title`, quoted `created` (today), `status: reading`,
   `author`, `summary`, `draft: false`, tags.

2. **Dates.** Go's time formatting uses a reference date rather than
   `yyyy-MM-dd`. It's `time.Now().Format("2006-01-02")`, those exact digits,
   which are the reference time `01/02 03:04:05PM '06 -0700`. It looks arbitrary
   because it is.

   Set the timezone explicitly with `time.LoadLocation("Europe/London")`. Your
   VPS is UTC, so a book you start at 23:30 in summer gets tomorrow's date. And
   add `import _ "time/tzdata"`, an underscore import that embeds the timezone
   database in the binary, because a minimal VPS image may not have one. (An
   underscore import means "run this package's initialisation, I don't need its
   name". You'll see it used for database driver registration too.)

3. Wire up `DRY_RUN`: when on, reply with the exact file content instead of
   calling `PutFile`. Use it until you trust the output.

4. Turn it off. Send yourself a title. Watch the page appear.

### If it goes wrong

Before you turn `DRY_RUN` off, know your escape hatch. For now it's manual and
it requires no code: pull the content repo locally, `git revert` the bot's
commit, push. The bot's commits are easy to spot because you control the commit
message, so make it something greppable like `bot: add the-testaments`.

Phase 6 automates this. But knowing the manual path exists is what makes the
first real write feel survivable, so don't skip reading it.

**Done when:** a real book page is live, under the right heading on the index,
showing the right date.

### Deliberately not yet

You haven't touched Open Library. That's on purpose: writing correct frontmatter
to a public site is the risky, irreversible part, and metadata lookup is a
convenience. Get the dangerous thing right while the inputs are hardcoded.

---

## Phase 6: knowing when you've broken the site

**Goal:** close the loop. This is the phase most side projects skip and then
regret.

### The bot is lying to you

Here is the failure you have right now. The bot writes a file, GitHub accepts
the commit, the bot replies "added!", and you put your phone down. Then GitHub
Actions starts a build, Quartz hits the malformed frontmatter your bot just
wrote, the build fails, and **the site stops deploying**. Not just that page:
every subsequent change to anything, silently blocked, until you happen to
notice.

The bot said it worked. From the bot's perspective it did work, because
"GitHub accepted my commit" and "the site built" are different facts and it only
checked the first. You might not find out for days.

A write is not done when the commit lands. It's done when the build goes green.

### Watching the build

The PUT response from Phase 4 includes a `commit` object. Take `commit.sha`,
then poll:

```
GET /repos/{owner}/{repo}/actions/runs?head_sha={sha}
```

The response has `workflow_runs`, an array of objects carrying `status`
(`queued`, `in_progress`, `completed`), `conclusion` (`success`, `failure`,
`cancelled`, or null while still running) and `html_url`.

**The polling pattern**, which generalises to every "wait for a remote thing"
problem you'll ever have:

1. **Tolerate an empty result at first.** For the first several seconds after
   your commit, `total_count` is 0 because the run doesn't exist yet. Empty
   means "not yet", not "no build". Treat it as a reason to keep waiting, not as
   an answer.
2. **Poll on a fixed interval**, about 10 seconds. There's no benefit to being
   faster than the thing you're watching, and you have a rate limit to respect.
3. **Have a deadline**, about 5 minutes. Use `context.WithTimeout` rather than
   counting iterations, so the deadline and cancellation are one mechanism
   rather than two. This matters at shutdown: a `SIGTERM` should abandon the
   watch, not wait out the timer.
4. **Decide what each outcome says.** Success: say nothing. You don't want a
   notification every time something works, or you'll stop reading them. Failure:
   message immediately, and **include `html_url`**, because the first thing you
   want is the log, and typing out a GitHub Actions URL on a phone is miserable.
   Timeout: say so plainly ("still building after 5 minutes, worth a look") with
   the same link. Never fail silently, and never claim success you didn't
   observe.

### Your first goroutine, and the rule for it

That watch takes up to five minutes. Your poll loop cannot sit there, or the bot
stops responding to you entirely. So this runs concurrently:

```go
go watchBuild(ctx, sha, chatID)
```

This is the only goroutine in the project, and it's safe for a specific reason
worth naming: **it shares no mutable state.** It receives a SHA, a chat ID and a
context by value, calls two HTTP clients, and sends a message. It never touches
the pending-actions map you'll build in Phase 7. Concurrency is only dangerous
when two goroutines touch the same memory, and this one touches none of yours.

Derive its context from your application's root context, not from a per-request
one, so it survives the handler returning but still dies on shutdown.

### An undo command

Now automate the escape hatch from Phase 5. Keep it deliberately small: **only
the most recent action**, held in memory, cleared on restart.

Two cases, and they're different:

- **Undoing a create** means deleting the file:
  `DELETE /repos/{owner}/{repo}/contents/{path}` with `message`, `sha` and
  `branch`. You need the file's blob SHA, which the create's response gave you
  in `content.sha`.
- **Undoing an update** means writing the previous content back. You already
  have it: `GetFile` returned it before you modified it. Keep those bytes around
  and `PutFile` them with the current SHA.

Resist making this a full history. An undo stack invites you to build a
`/redo`, and the honest fallback for anything older is `git revert`, which
already exists, works perfectly, and cost you nothing.

**Done when:** you deliberately write a note with a broken `status`, and the bot
messages you with a failing-build link within a couple of minutes. Then `/undo`
removes it and the next build goes green.

---

## Phase 7: metadata lookup and buttons

**Goal:** make it pleasant. Conversation state, inline keyboards.

### Open Library

No API key needed:

```
https://openlibrary.org/search.json?title=<title>&fields=title,author_name,first_publish_year,cover_i&limit=5
```

Use `title=` rather than `q=`, the ordering is noticeably better. `author_name`
is an array. Show the top three as buttons.

**Never let this block a create.** Open Library is bad at obscure books, and
being unable to add a book because a free API shrugged is infuriating. Always
offer "none of these, use what I typed".

### Inline keyboards

Attach `reply_markup` to `sendMessage`:

```json
{"inline_keyboard": [[{"text": "Mark finished", "callback_data": "abc123"}]]}
```

Tapping sends you an `Update` with a `CallbackQuery` instead of a `Message`.
Three things:

- **`callback_data` is capped at 64 bytes.** Don't encode an action plus a slug
  in there: `the-brief-wondrous-life-of-oscar-wao` alone nearly fills it. Store
  the pending action in an in-memory map keyed by a short random ID and put
  *that* in the button.
- **You must call `answerCallbackQuery`**, or the user's button spins until it
  times out. Even with no text.
- **Allowlist the callback path too.** It's `update.CallbackQuery.From.ID`, a
  different field from the message path. Guarding messages and forgetting
  callbacks is easy, and callbacks are what actually write to your site.

### State and restarts

In-memory state means a restart drops pending confirmations. Don't fight it:
when a callback ID isn't in the map, reply "that expired, send the title again".
A clear message beats a mysterious silence, and it's three lines.

### Why that map needs no mutex

You've just added shared mutable state, which in most languages is where you
start reaching for locks. Here you don't need one, and understanding *why* is
worth more than the lock would be.

Your poll loop receives updates and handles them **one at a time, in a single
goroutine.** Nothing else touches the map. One goroutine cannot race itself, so
there is no lock to take. Adding a `sync.Mutex` here would be pure noise: it
would never be contended, it would suggest to a future reader that concurrent
access happens, and it would hide the actual design decision.

The design decision is: *this bot handles one message at a time, and that's
fine.* You are the only user. Serialising your own messages costs nothing and
removes an entire category of bug.

**What changes that.** The moment you write `go handleUpdate(u)`, because
something felt slow, you have a data race. Go maps are explicitly not safe for
concurrent use, and the runtime will usually catch you with a
`fatal error: concurrent map writes`, which is an immediate crash, not a
recoverable panic. Your bot dies mid-conversation.

If you ever do need it, the tools are `sync.Mutex` (wrap the map in a small
struct with methods, never expose the bare map) or a channel that serialises
access. For guarding a map, prefer the mutex. Channels are for handing off work
between goroutines, not for protecting state, and using them as locks is a
common early misuse.

Run `go test -race ./...` regularly regardless. The race detector finds real
bugs and costs you nothing while there's nothing to find.

Contrast this with the build watcher in Phase 6: also concurrent, also
lock-free, and safe for the same underlying reason (it shares nothing). Knowing
when you *don't* need a concurrency primitive is a better lesson than learning
the primitive.

**Done when:** you send "testaments", pick from a keyboard, and a page appears.

---

## Phase 8: find and update an existing book

**Goal:** fuzzy matching, and knowing when to stop.

You'll type "testaments", not "The Testaments".

**Don't build a title index.** `ListDir` on `content/reading` gives you every
filename in one call, always fresh, nothing to invalidate. The filenames *are*
slugs, so match against those, then fetch only the one file the user picks.

Start with `strings.Contains` on the slug. Genuinely, try it for a week. You
have maybe a hundred books and you know their titles, so substring matching will
be right almost always. Add Levenshtein only when you can name a real case where
it failed. Reaching for a fuzzy-matching library on day one is the classic
version of this mistake.

Handle: no match (offer to create), one match (go straight to actions), several
(disambiguation keyboard).

### What an update writes

- **Status:** a frontmatter edit via `SetField`.
- **Finishing:** also write `finished: "YYYY-MM-DD"`. Cheap now,
  unreconstructable later, and it's the most interesting field in a reading log
  a year from now.
- **Rating:** `rating: 8` in frontmatter, *not* buried in the summary prose.
  Quartz ignores frontmatter it doesn't recognise, so this costs nothing today
  and you can surface it in the template later without touching the bot.
  Structured data belongs in structured fields.
- **Progress note:** `AppendToLog`.

Every one of these goes through the conflict-retry path from Phase 4, and this
is where it earns its keep: updates race `obsidian-git` far more often than
creates do, because you're editing books you're actively reading.

**Done when:** "testaments", tap *finished*, tap *8/10*, and the index moves it
under the right heading.

---

## Phase 9: deploy properly

**Goal:** it survives a reboot.

1. **Build for the VPS:**
   `GOOS=linux GOARCH=amd64 go build -o reading-bot ./cmd/reading-bot`. No
   runtime, no dependencies, which is why Go suits this job. `scp` it up.

2. **Secrets go in an EnvironmentFile**, mode `0600`, owned by the service user,
   *not* inline in the unit file, which is world-readable.

3. **A systemd unit** with `Restart=always`, `RestartSec=10`, and the hardening
   basics: `DynamicUser=yes`, `NoNewPrivileges=yes`, `ProtectSystem=strict`,
   `PrivateTmp=yes`. Read `man systemd.exec` for what each does rather than
   pasting blind. A chunk of this project's value is being able to explain it.

4. **Graceful shutdown.** This is where `context` earns its place. Catch
   `SIGTERM` with `signal.NotifyContext`, cancel the context, let the poll loop
   exit cleanly. You built for this in Phase 2 (the loop exits on a cancelled
   context rather than backing off) and Phase 6 (the build watcher dies with the
   root context). Now check both actually work.

5. **Logs:** `journalctl -u reading-bot -f`. Log slugs, never note bodies. The
   failure mode is pasting a log into a public issue and shipping a page's
   contents with it.

**Done when:** `systemctl restart` and a VPS reboot both leave you with a
working bot, and a restart mid-build-watch doesn't leave a stuck process.

---

## Phase 10: polish

Only once it's been used for real, in rough priority order:

- **Covers.** Store `cover: https://covers.openlibrary.org/b/id/<id>-L.jpg` in
  frontmatter. Don't download the image: the Contents API writes one file per
  call, so the note and the image can't be one commit, and you've added binaries
  to your content repo for nothing.
- **Batch edits.** Right now three taps is three commits, three builds and three
  build-watch goroutines. Stage changes in memory and commit once on "done".
- **`obsidian-git` friction.** The bot pushes, so your vault is behind until it
  pulls. Set the plugin to auto-pull on an interval. This also reduces how often
  you hit the Phase 4 conflict path.
- **A `/status` command** listing what you're currently reading. Read-only,
  quick, and surprisingly useful.

---

## How to use AI on this without it doing it for you

You'll be tempted to paste a phase into a chat and get working code. That
produces a bot and no learning. Some things that work better:

**Ask for the concept, not the code.** "Explain Go's pointer vs value receivers
using my config loader as the example" teaches you something. "Write SetField
for me" doesn't.

**Write it badly first, then ask for a review.** Attempt it, get it compiling,
then ask "here's my `AppendToLog`, what would a Go reviewer say?" You'll learn
more from having your own code critiqued than from reading someone else's.

**Ask "why doesn't this compile?" and make it explain**, rather than asking for
a fix. Go's errors around pointers, interfaces and nil are unfamiliar but
consistent, so understanding one teaches you a category.

**Get it to write tests against a signature, then make them pass.** Genuinely
good division of labour: the test encodes the requirement, you write the
implementation. That's what Phase 3 is set up for.

**Ask it to argue with you.** "I'm going to store the rating in the summary
field, what breaks in six months?" A good answer changes your design, and a bad
one you'll spot, which is its own kind of progress.

The tell that you've gone too far: you can't explain a file you're about to
commit. Stop and go back a step.

---

## Appendix: getting unstuck in Go

- **`go doc <pkg> <symbol>`** for offline docs, e.g. `go doc strings Builder`.
- **[pkg.go.dev](https://pkg.go.dev)** for the standard library, well indexed.
  You'll need `net/http`, `net/http/httptest`, `encoding/json`, `strings`,
  `time` and `context`.
- **[Go by Example](https://gobyexample.com)** for short runnable snippets. The
  fastest way to remember syntax you've half-forgotten.
- **`gofmt`**: never argue about formatting, there's one style.
- **`nil pointer dereference`**: you dereferenced an optional field. In this
  project it's almost always `update.Message` on an update that was actually a
  callback.
- **`declared and not used`**: Go refuses to compile unused local variables.
  This is deliberate and you'll come to like it.
- **`fatal error: concurrent map writes`**: you added a `go` somewhere. See
  Phase 7.
