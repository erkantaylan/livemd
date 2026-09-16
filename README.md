# LiveMD

Live file viewer with syntax highlighting, powered by CLI file watching.

## Problem

Reading markdown in the terminal is painful - no formatting, code blocks are plain text, tables are unreadable. When AI agents generate markdown files, you want to **see** them properly rendered.

## Solution

LiveMD runs as a persistent server. Add markdown files to watch from the CLI, see them rendered in your browser with live updates.

```
Terminal                              Browser (localhost:3000)
────────────────────────────────────────────────────────────────
$ livemd start                    →   Server started

$ livemd add README.md            →   Sidebar shows README.md
                                      Content rendered on right

$ livemd add docs/guide.md        →   Two files in sidebar
                                      Click to switch

[edit README.md]                  →   Browser updates live
```

## Install

The installers are **idempotent** — re-running them updates an existing install in place. Once livemd is on your machine you can also self-update with `livemd install`.

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/erkantaylan/livemd/master/install.sh | sudo bash
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/erkantaylan/livemd/master/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\Programs\livemd\` and adds it to your user `PATH`. No admin required.

### From source

```bash
git clone https://github.com/erkantaylan/livemd.git
cd livemd
make install              # build, install, start daemon
make install PORT=3001    # same, but on port 3001
```

`make install` works as both first-time install and update — it stops any running daemon, replaces the binary, and starts the new one.

### Custom port

- Make: `make install PORT=3001`
- Curl/iex: `LIVEMD_PORT=3001 curl ... | sudo bash` or `$env:LIVEMD_PORT=3001; irm ... | iex`
- Or set persistently any time: `livemd port 3001`

## Usage

The installer above already starts the server in the background. Otherwise:

```bash
# Start the server (foreground — Ctrl+C to stop)
livemd start

# Or as a background daemon
livemd start --detach

# Add files to watch
livemd add README.md
livemd add docs/guide.md

# Follow a folder (its Refresh button picks up files added later)
livemd add ./docs -r
livemd add ./src -r --filter "md,go,js"
livemd add ./misc -r --depth 5         # cap depth in non-git folders

# List watched files
livemd list

# Remove a file
livemd remove README.md

# Stop the server
livemd stop
```

Open http://localhost:3000 in your browser.

### Start at login

`livemd start --detach` lasts until you log out or reboot. To have livemd start
by itself every time you log in:

```bash
livemd service install     # set up autostart and start livemd now
livemd service status      # is autostart installed and enabled, is livemd running
livemd service uninstall   # remove autostart and stop livemd
```

- **Linux** uses a systemd user unit (`~/.config/systemd/user/livemd.service`)
  that restarts livemd if it crashes. While the unit is installed,
  `livemd start --detach` and `livemd install` start and stop livemd through
  systemd, so no second copy runs outside it. Logs: `journalctl --user -u livemd`.
- **Windows** adds a logon entry under
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, which needs no admin
  rights and shows up in Task Manager's Startup tab. It is not restarted after a
  crash, and a console window may flash briefly at logon.

Run `livemd service install` again after moving the livemd binary.

## Make Commands

```
make              Show help
make build        Build the binary in the current directory
make clean        Remove the local build

make install      Build, install, and start daemon (idempotent — also updates)
make install PORT=3001    Same, but on a specific port
make uninstall    Stop daemon and remove binary

make start        Start the daemon (assumes already installed)
make stop         Stop the daemon

make watch f1 f2      Add files to watch
make watch-dir ./dir  Add folder recursively
make unwatch f1       Remove files from watch
make list             List watched files
```

## Demo

The repo ships a feature showcase that exercises every viewer — GFM tables and
task lists, syntax-highlighted code, KaTeX math (inline + display), 12 mermaid
diagram types plus oversized/invalid ones for limit testing, embedded images,
and inline SVG:

```bash
livemd add docs/demo.md
```

## Features

- **Persistent server** - Start once, add files anytime; state survives restart
- **Followed folders** - `livemd add ./dir -r` registers everything in a directory; gitignored files are skipped automatically when the folder is in a git repo. A Refresh button on each followed folder picks up files added since
- **Tree view sidebar** - Collapsible folder structure with a Refresh button on followed folders
- **Lazy watching** - Files are registered but only actively watched when selected, and content is rendered on demand: the daemon holds no HTML, so tracking a 40 MB file costs nothing until you open it
- **Many viewers** - Markdown (GFM + mermaid + KaTeX math), 50+ syntax-highlighted code languages, images, PDFs, audio, video, CSV/TSV as tables
- **Preview / Raw toggle** - Markdown and HTML switch between the rendered document and the original source; the choice is remembered per file and travels in the URL
- **Normal / Wide toggle** - Documents read in a centred column by default; Wide lets them fill the window, for wide tables and long code. Remembered across restarts
- **Copy-pasteable URLs** - The address bar *is* the file's path (`http://localhost:3000/home/me/notes.md`), so a path pasted from a terminal opens it and a link copied from the page is a path; `?view=raw` and `#heading` ride along, and Back/Forward step through the documents you opened
- **Working markdown links** - Relative links and images resolve against the document's own directory. A link to a neighbour opens it in place without adding it to the watch list; a Track button promotes it when you want live reload. Links outside every tracked root are refused rather than followed
- **Add from the browser** - Track a file or folder by typing its path in the sidebar
- **No line limits** - Files render in full, with a Copy button for the whole file. Text files are capped at 50 MB (media is exempt, since the browser streams it); syntax highlighting drops to plain text above 2 MB, where it costs 8x the HTML and buys nothing
- **WebSocket live updates** - No page refresh needed
- **Rendered changelog** - The Changelog tab shows GitHub release notes as markdown, not raw text
- **Self-update** - `livemd install` pulls the latest GitHub release in place
- **Cross-platform** - Linux, macOS, Windows (background daemon on all three)

## Tech Stack

- Go single binary (~15MB)
- [goldmark](https://github.com/yuin/goldmark) for markdown parsing
- [chroma](https://github.com/alecthomas/chroma) for syntax highlighting
- [fsnotify](https://github.com/fsnotify/fsnotify) for file watching
- No CSS framework - the interface is styled from `static/style.css`; see [design.md](design.md)
- [gorilla/websocket](https://github.com/gorilla/websocket) for live updates
