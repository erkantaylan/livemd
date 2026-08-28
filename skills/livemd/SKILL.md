---
name: livemd
description: Render a file in the browser with live reload via the livemd server, instead of dumping it into the terminal - markdown, code, CSV, images, PDFs, media. Opt-in only - invoke when the user explicitly runs /livemd, or asks by name to livemd / preview / render / watch a file or folder, or asks about livemd server lifecycle (start, stop, list, remove, port, install). Do NOT invoke on your own initiative just because a file was generated.
---

# LiveMD

`livemd` is a persistent local server (default http://localhost:3000) that watches
files and renders them in the browser with WebSocket live updates: GFM tables,
mermaid diagrams, KaTeX math, syntax highlighting, images, PDFs, CSV as tables.

## Activation

This skill is **opt-in**. Use it only when the user has asked for it:

- They ran `/livemd`.
- They named livemd ("livemd this", "add it to livemd", "is the livemd server up?").
- They asked to preview / render / open / watch a specific file *in the browser*.

Do not reach for livemd unprompted after generating a report, plan, or doc. Hand
those over the normal way unless the user asks to see them rendered.

## Core commands

```bash
livemd start --detach          # start the daemon in the background
livemd add README.md           # track and render a file
livemd add ./docs -r           # follow a folder, auto-adding new files
livemd add ./src -r --filter "md,go,js"
livemd add ./misc -r --depth 5 # cap depth outside git repos
livemd list                    # what is currently tracked
livemd remove README.md        # stop tracking
livemd stop                    # stop the daemon
livemd port 3001               # change the port persistently
livemd install                 # self-update from the latest GitHub release
```

## Standard flow

1. `livemd list` — if it errors, the daemon is not running; `livemd start --detach`.
   The output also tells you the active port; do not assume 3000.
2. `livemd add <path>` for the file(s) the user wants to see.
3. Hand back a deep link in one line:
   `http://localhost:3000/?file=<path>` (add `&view=raw` when the source matters
   more than the rendering).

That link is the whole handoff. Do not also paste the file contents into the
terminal — displaying it twice is the thing livemd exists to avoid.

## Paths this machine keeps tracked

Long-lived documents that are read in the browser rather than the terminal, and that change under
you while you are reading them. Add them once; the daemon picks up every write after that.

```bash
livemd add /path/to/a/decision-record.md
livemd add /path/to/agent-notes -r --filter md
```

**This section is deliberately empty of real paths.** The copy in the repo is the one other people
install, and a machine's project layout is not something to ship them — the local copy under
`~/.claude/skills/livemd/` is where the actual paths go. Write yours there, with a line each saying
what makes the file worth live reload rather than a `cat`.

The two that earn it in practice: a document **amended in place** rather than appended to, so it
grows in the middle at the point you are already reading; and a **folder something else writes**,
followed so a new file appears without another `add`.

## Notes and gotchas

- Adding a file is cheap. The daemon registers it but only watches and renders on
  demand, so tracking a large file costs nothing until it is opened.
- Gitignored files are skipped automatically when a followed folder is inside a
  git repo.
- Text files are capped at 50 MB; syntax highlighting drops to plain text above
  2 MB. Media is exempt — the browser streams it.
- Opening a URL for an untracked file tracks it automatically, so a deep link is
  a valid substitute for `livemd add` when the user will click it anyway.
- Paths resolve relative to the shell's working directory; prefer absolute paths
  when adding files from outside the project root.

## Installing this skill on another machine

This file is the original, committed at `skills/livemd/SKILL.md` in the livemd
repo. To make `/livemd` available globally elsewhere, copy the directory:

```bash
mkdir -p ~/.claude/skills
cp -r /path/to/livemd/skills/livemd ~/.claude/skills/livemd
```

Re-copy after editing the repo copy to keep the two in sync.
