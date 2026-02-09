.PHONY: help build run install install-user uninstall uninstall-user clean start stop list watch watch-dir unwatch daemon daemon-stop update

.DEFAULT_GOAL := help

# Detect OS for binary name
ifeq ($(OS),Windows_NT)
    BINARY = livemd.exe
    RM = del /F /Q
else
    BINARY = livemd
    RM = rm -f
endif

# Installation paths
PREFIX ?= /usr/local
USER_PREFIX ?= $(HOME)/.local
INSTALL_DIR = $(PREFIX)/bin
USER_INSTALL_DIR = $(USER_PREFIX)/bin

# Runtime directory for PID file (XDG spec)
XDG_RUNTIME_DIR ?= /tmp
PID_FILE = $(XDG_RUNTIME_DIR)/livemd.pid

# Capture extra arguments for watch/unwatch
ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
$(eval $(ARGS):;@:)

# Version from git tag (fallback to dev)
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

# Build the binary
build:
	go build -buildvcs=false -ldflags="-X main.Version=$(VERSION)" -o $(BINARY) .

# Start the server
run: build
	./$(BINARY) start

# Install globally (requires sudo)
install: build
ifeq ($(OS),Windows_NT)
	@echo On Windows, copy $(BINARY) to a directory in your PATH manually
else
	@echo Installing to $(INSTALL_DIR)/ ...
	install -d $(INSTALL_DIR)
	install -m 755 $(BINARY) $(INSTALL_DIR)/
	@echo Done. Run 'livemd start' to start the server.
endif

# Install to user directory (no sudo required)
install-user: build
ifeq ($(OS),Windows_NT)
	@echo On Windows, copy $(BINARY) to a directory in your PATH manually
else
	@echo Installing to $(USER_INSTALL_DIR)/ ...
	@mkdir -p $(USER_INSTALL_DIR)
	@cp $(BINARY) $(USER_INSTALL_DIR)/
	@chmod 755 $(USER_INSTALL_DIR)/$(BINARY)
	@echo Done. Make sure $(USER_INSTALL_DIR) is in your PATH.
	@echo Run 'livemd start' to start the server.
endif

# Uninstall from global location
uninstall:
ifeq ($(OS),Windows_NT)
	@echo On Windows, remove $(BINARY) from your PATH manually
else
	@echo Removing $(INSTALL_DIR)/$(BINARY) ...
	$(RM) $(INSTALL_DIR)/$(BINARY)
	@echo Done.
endif

# Uninstall from user location
uninstall-user:
ifeq ($(OS),Windows_NT)
	@echo On Windows, remove $(BINARY) from your PATH manually
else
	@echo Removing $(USER_INSTALL_DIR)/$(BINARY) ...
	$(RM) $(USER_INSTALL_DIR)/$(BINARY)
	@echo Done.
endif

# Clean build artifacts
clean:
	$(RM) $(BINARY)

# Server commands
start: build
	./$(BINARY) start

stop:
	./$(BINARY) stop

list:
	./$(BINARY) list

# Watch files: make watch file1.md file2.md
watch:
ifeq ($(ARGS),)
	@echo Usage: make watch file1.md file2.md ...
else
ifeq ($(OS),Windows_NT)
	@for %%f in ($(ARGS)) do $(BINARY) add %%f
else
	@for f in $(ARGS); do ./$(BINARY) add $$f; done
endif
endif

# Watch folder recursively: make watch-dir ./docs
watch-dir:
ifeq ($(ARGS),)
	@echo Usage: make watch-dir ./folder
else
ifeq ($(OS),Windows_NT)
	@echo Recursive watch not supported on Windows via make
else
	./$(BINARY) add $(firstword $(ARGS)) -r
endif
endif

# Unwatch files: make unwatch file1.md file2.md
unwatch:
ifeq ($(ARGS),)
	@echo Usage: make unwatch file1.md file2.md ...
else
ifeq ($(OS),Windows_NT)
	@for %%f in ($(ARGS)) do $(BINARY) remove %%f
else
	@for f in $(ARGS); do ./$(BINARY) remove $$f; done
endif
endif

# Run as daemon (background service)
daemon: build
ifeq ($(OS),Windows_NT)
	@echo Daemon mode not supported on Windows
else
	@if [ -f "$(PID_FILE)" ] && kill -0 $$(cat "$(PID_FILE)") 2>/dev/null; then \
		echo "Daemon already running (PID $$(cat $(PID_FILE)))"; \
	else \
		echo "Starting livemd daemon..."; \
		nohup ./$(BINARY) start > /dev/null 2>&1 & \
		echo $$! > "$(PID_FILE)"; \
		echo "Daemon started (PID $$!)"; \
		echo "PID file: $(PID_FILE)"; \
	fi
endif

# Stop the daemon
daemon-stop:
ifeq ($(OS),Windows_NT)
	@echo Daemon mode not supported on Windows
else
	@if [ -f "$(PID_FILE)" ]; then \
		PID=$$(cat "$(PID_FILE)"); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "Stopping daemon (PID $$PID)..."; \
			kill $$PID; \
			rm -f "$(PID_FILE)"; \
			echo "Daemon stopped."; \
		else \
			echo "Daemon not running (stale PID file)."; \
			rm -f "$(PID_FILE)"; \
		fi \
	else \
		echo "No daemon running (no PID file found)."; \
	fi
endif

# Update: pull latest, rebuild, reinstall
update:
ifeq ($(OS),Windows_NT)
	@echo Update not supported on Windows
else
	@echo "Pulling latest changes..."
	git pull
	@echo "Rebuilding..."
	$(MAKE) build
	@echo "Update complete. Run 'make install' or 'make install-user' to reinstall."
endif

# Show help
help:
	@echo LiveMD - Live markdown viewer
	@echo ---
	@echo Build:
	@echo "  make build ............. Build binary"
	@echo "  make clean ............. Remove binary"
	@echo ---
	@echo Install:
	@echo "  make install ........... Install to /usr/local/bin (sudo)"
	@echo "  make install-user ...... Install to ~/.local/bin (no sudo)"
	@echo "  make uninstall ......... Remove from /usr/local/bin"
	@echo "  make uninstall-user .... Remove from ~/.local/bin"
	@echo "  make update ............ Pull, rebuild (reinstall manually)"
	@echo ---
	@echo Server:
	@echo "  make start ............. Start server (foreground)"
	@echo "  make stop .............. Stop server"
	@echo "  make daemon ............ Start as background daemon"
	@echo "  make daemon-stop ....... Stop background daemon"
	@echo ---
	@echo Files:
	@echo "  make watch f1 f2 ....... Add files to watch"
	@echo "  make watch-dir ./dir ... Add folder recursively"
	@echo "  make unwatch f1 f2 ..... Remove files"
	@echo "  make list .............. List watched files"
