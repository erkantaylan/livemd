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
make install      Install to /usr/local/bin
make clean        Remove binary

make start        Start the server
make stop         Stop the server

make watch f1 f2  Add files to watch
make unwatch f1   Remove files from watch
make list         List watched files
```

## Features

- **Persistent server** - Start once, add files anytime
- **Sidebar UI** - File list with tracking and last change times
- **WebSocket live updates** - No page refresh needed
- **GitHub-flavored markdown** - Tables, task lists, autolinks
- **Syntax highlighting** - Code blocks in markdown and standalone code files (50+ languages)
- **Cross-platform** - Works on Linux, macOS, Windows

## Tech Stack

- Go single binary (~15MB)
- [goldmark](https://github.com/yuin/goldmark) for markdown parsing
- [chroma](https://github.com/alecthomas/chroma) for syntax highlighting
- [fsnotify](https://github.com/fsnotify/fsnotify) for file watching
- [gorilla/websocket](https://github.com/gorilla/websocket) for live updates
