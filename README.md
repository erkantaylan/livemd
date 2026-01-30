# LiveMD

Live markdown viewer powered by CLI file watching.

## Problem

Reading markdown in the terminal is painful - no formatting, code blocks are plain text, tables are unreadable. When AI agents generate markdown files, you want to **see** them properly rendered.

## Solution

LiveMD watches markdown files and serves them to your browser with live updates. No page refresh needed.

```
Terminal                          Browser (localhost:3000)
─────────────────────────────────────────────────────────
$ livemd README.md            →   Rendered markdown
                                  - Headers formatted
                                  - Code highlighted
                                  - Tables rendered
                                  - Updates on save
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
# Watch a file
livemd README.md

# Custom port
livemd --port 8080 docs/guide.md
```

Then open http://localhost:3000 in your browser.

## Make Commands

```
make              Show help
make build        Build the binary
make run          Build and run with README.md
make run FILE=x   Run with a specific file
make run PORT=x   Run on a different port
make clean        Remove binary
```

## Features

- WebSocket live updates (no refresh)
- GitHub-flavored markdown (tables, task lists, autolinks)
- Syntax highlighting for code blocks
- Scroll position preserved on updates
- Auto-reconnect if server restarts
- Graceful shutdown (Ctrl+C)
- Cross-platform (Linux, macOS, Windows)

## Tech Stack

- Go single binary (~15MB)
- [goldmark](https://github.com/yuin/goldmark) for markdown parsing
- [fsnotify](https://github.com/fsnotify/fsnotify) for file watching
- [gorilla/websocket](https://github.com/gorilla/websocket) for live updates
