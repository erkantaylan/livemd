.PHONY: help build run install clean start stop add remove list

.DEFAULT_GOAL := help

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
	go build -buildvcs=false -o $(BINARY) .

# Start the server
run: build
	./$(BINARY) start

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

# Server commands
start: build
	./$(BINARY) start

stop:
	./$(BINARY) stop

add:
ifndef FILE
	@echo "Usage: make add FILE=path/to/file.md"
else
	./$(BINARY) add $(FILE)
endif

remove:
ifndef FILE
	@echo "Usage: make remove FILE=path/to/file.md"
else
	./$(BINARY) remove $(FILE)
endif

list:
	./$(BINARY) list

# Show help
help:
	@echo LiveMD - Live markdown viewer
	@echo ---
	@echo   build ........ Build the binary
	@echo   install ...... Install to PATH
	@echo   clean ........ Remove binary
	@echo   start ........ Start server
	@echo   stop ......... Stop server
	@echo   add FILE=x ... Add file to watch
	@echo   remove FILE=x. Remove file
	@echo   list ......... List watched files
