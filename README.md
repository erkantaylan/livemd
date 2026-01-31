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

```bash
# Clone and build
git clone https://github.com/erkantaylan/live-md.git
cd live-md
make build

# Optional: install globally
make install
```

## Usage

```bash
# Start the server
livemd start

# Add files to watch
livemd add README.md
livemd add docs/guide.md

# Add entire folder recursively
livemd add ./docs -r
livemd add ./src -r --filter "md,go,js"

# List watched files
livemd list

# Remove a file
livemd remove README.md

# Stop the server
livemd stop
```

Open http://localhost:3000 in your browser.

## Make Commands

```
make              Show help
make build        Build the binary
make clean        Remove binary

make install      Install to /usr/local/bin (sudo)
make install-user Install to ~/.local/bin (no sudo)
make uninstall    Remove from /usr/local/bin
make uninstall-user Remove from ~/.local/bin
make update       Pull latest and rebuild

make start        Start the server (foreground)
make stop         Stop the server
make daemon       Start as background daemon
make daemon-stop  Stop background daemon

make watch f1 f2      Add files to watch
make watch-dir ./dir  Add folder recursively
make unwatch f1       Remove files from watch
make list             List watched files
```

## Features

- **Persistent server** - Start once, add files anytime
- **Tree view sidebar** - Collapsible folder structure like a solution explorer
- **Lazy watching** - Files are registered but only actively watched when selected (saves system resources)
- **Recursive folder watching** - Add entire directories with `livemd add ./folder -r`
- **WebSocket live updates** - No page refresh needed
- **GitHub-flavored markdown** - Tables, task lists, autolinks
- **Syntax highlighting** - Code blocks in markdown and standalone code files (50+ languages)
- **Network access** - Shows all network interface IPs on startup for easy access from other devices
- **Cross-platform** - Works on Linux, macOS, Windows

## Tech Stack

- Go single binary (~15MB)
- [goldmark](https://github.com/yuin/goldmark) for markdown parsing
- [chroma](https://github.com/alecthomas/chroma) for syntax highlighting
- [fsnotify](https://github.com/fsnotify/fsnotify) for file watching
- [gorilla/websocket](https://github.com/gorilla/websocket) for live updates
