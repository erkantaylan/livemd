.PHONY: help build run install clean start stop list watch unwatch

.DEFAULT_GOAL := help

# Detect OS for binary name
ifeq ($(OS),Windows_NT)
    BINARY = livemd.exe
    RM = del /F /Q
else
    BINARY = livemd
    RM = rm -f
endif

# Capture extra arguments for watch/unwatch
ARGS := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
$(eval $(ARGS):;@:)

# Build the binary
build:
	go build -buildvcs=false -o $(BINARY) .

# Start the server
run: build
	./$(BINARY) start

# Install globally (Unix only)
install: build
ifeq ($(OS),Windows_NT)
	@echo On Windows, copy $(BINARY) to a directory in your PATH manually
else
	cp $(BINARY) /usr/local/bin/
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

# Show help
help:
	@echo LiveMD - Live markdown viewer
	@echo ---
	@echo Build:
	@echo   make build .......... Build binary
	@echo   make install ........ Install to PATH
	@echo   make clean .......... Remove binary
	@echo ---
	@echo Server:
	@echo   make start .......... Start server
	@echo   make stop ........... Stop server
	@echo ---
	@echo Files:
	@echo   make watch f1 f2 .... Add files to watch
	@echo   make unwatch f1 f2 .. Remove files
	@echo   make list ........... List watched files
