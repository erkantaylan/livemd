.PHONY: build run install clean

# Default file to watch
FILE ?= README.md
PORT ?= 3000

# Detect OS for binary name
ifeq ($(OS),Windows_NT)
    BINARY = livemd.exe
    RM = del /F /Q
else
    BINARY = livemd
    RM = rm -f
endif

# Build the binary
build:
	go build -o $(BINARY) .

# Run with a file (usage: make run FILE=docs/guide.md)
run: build
	./$(BINARY) --port $(PORT) $(FILE)

# Install globally (Unix only)
install: build
ifeq ($(OS),Windows_NT)
	@echo "On Windows, copy $(BINARY) to a directory in your PATH manually"
else
	cp $(BINARY) /usr/local/bin/
endif

# Clean build artifacts
clean:
	$(RM) $(BINARY)
