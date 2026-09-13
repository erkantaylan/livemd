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
livemd service install         # start at login (systemd / Windows logon entry)
livemd service status          # autostart installed? enabled? running?
livemd install                 # self-update from the latest GitHub release
```

## Standard flow

1. `livemd list` — if it errors, the daemon is not running; `livemd start --detach`
   (from v1.0.0 it starts the systemd unit instead when one is installed). The
   output also tells you the active port; do not assume 3000.
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

## Running as a service

`livemd start --detach` lasts until logout or reboot. `livemd service install`
makes livemd start at every login, and starts it now:

- **Linux:** a systemd user unit, `~/.config/systemd/user/livemd.service`, that
  restarts livemd after a crash. While it is installed, `livemd start --detach`
  and `livemd install` go through systemd, so a copy outside the unit cannot
  happen. Logs: `journalctl --user -u livemd`.
- **Windows:** a logon entry, `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\livemd`.
  No admin rights, no restart after a crash, and it can be switched off in Task
  Manager's Startup tab, which `livemd service status` reports as `Enabled: no`.

`livemd service status` shows whether autostart is installed and enabled and
whether livemd is running. It also notes when the entry points somewhere else
than the current binary; run `livemd service install` again to fix that.
`livemd service uninstall` removes autostart and stops livemd. On versions
before v1.0.0 none of this exists: `livemd install` there relaunches the daemon
with `start --detach`, outside any service you set up by hand.

## The state file

The watch list lives in `~/.livemd-state.json` (`%APPDATA%\livemd-state.json` on
Windows): a `files` array and a `folders` array.

- The daemon rewrites it from memory on every change, so an edit made while it runs
  is overwritten. To edit by hand: stop the daemon, copy the file to a timestamped
  backup (a fixed backup name gets overwritten by the next session that makes one),
  edit, start.
- Followed folders are expanded into `files` at startup, so `livemd list` shows
  more entries than were added explicitly.
- The browser sidebar's remove buttons change it too. If entries are missing, check
  whether someone removed them there before suspecting the CLI.
- From v0.16.0, a file that fails to parse is moved to
  `~/.livemd-state.json.corrupt-<time>` instead of being replaced by an empty list,
  and followed folders survive restarts (earlier versions dropped them from the
  file on every start).

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
- `livemd add <folder>` requires `-r`; the CLI cannot follow a folder
  non-recursively.
- Adding a folder that is already followed searches it again, bringing back
  files removed from the list and applying the new options (from v1.1.0; before
  that it did nothing). Removing a folder in the sidebar also unfollows folders
  followed inside it.
- If `livemd start` says "already running" but nothing answers on that port, the
  lock file (`/tmp/livemd.lock`) was left by a daemon that died. v0.16.0 and later
  clear it automatically; on older versions delete it by hand.

## Installing this skill on another machine

This file is the original, committed at `skills/livemd/SKILL.md` in the livemd
repo. To make `/livemd` available globally elsewhere, copy the directory:

```bash
mkdir -p ~/.claude/skills
cp -r /path/to/livemd/skills/livemd ~/.claude/skills/livemd
```

Re-copy after editing the repo copy to keep the two in sync, carrying over the
local copy's own paths and machine setup rather than overwriting them.
