// Package main implements the LiveMD command-line tool for live markdown preview.
//
// LiveMD is a local development server that renders markdown files in real-time,
// automatically refreshing the browser when files change. It supports watching
// individual files or entire directories with configurable file type filtering.
//
// # Architecture
//
// The application follows a client-server model:
//   - Server process: Started with 'livemd start', runs in foreground serving HTTP/WebSocket
//   - CLI commands: Communicate with server via HTTP API (add, remove, list, stop)
//   - Lock file: Stores server port for CLI-server communication (~/.livemd.lock)
//
// # Commands
//
//   - start: Launch the server on specified port (default 3000)
//   - add: Add file(s) to watch list, supports recursive directory scanning
//   - remove: Stop watching a specific file
//   - list: Display all currently watched files
//   - stop: Gracefully shutdown the server
//
// # Usage
//
//	livemd start              # Start server
//	livemd add README.md      # Watch a file
//	livemd add ./docs -r      # Watch directory recursively
//	livemd stop               # Stop server
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// defaultExtensions defines the file types watched when recursively adding directories.
// These extensions cover common documentation, code, and configuration files that
// developers typically want to preview or monitor during development.
var defaultExtensions = []string{
	".md", ".markdown",
	".go",
	".cs", ".razor",
	".js", ".ts", ".jsx", ".tsx",
	".html", ".htm", ".css",
	".json", ".yaml", ".yml", ".toml",
	".py", ".rb", ".rs", ".java",
	".sh", ".bash",
	".xml", ".svg",
	".txt",
}

// Version is set at build time via -ldflags "-X main.Version=vX.Y.Z"
var Version = "dev"

// main is the entry point for the livemd CLI tool.
// It parses the first argument as a command and dispatches to the appropriate handler.
// If no command is provided or an unknown command is given, it displays usage information.
func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `LiveMD - Live markdown viewer (%s)

Usage:
  livemd start [--port PORT]    Start the server
  livemd add <file.md>          Add file to watch
  livemd add <folder> -r        Add folder recursively
  livemd remove <file.md>       Remove file from watch
  livemd list                   List watched files
  livemd stop                   Stop the server
  livemd port                   Show current port
  livemd port <number>          Set default port
  livemd version                Print version
  livemd update                 Update to latest release

Options:
  --port PORT    Port to serve on (default 3000)
  -r, --recursive   Recursively add files from folder
  --filter EXT      Filter by extensions (comma-separated, e.g. "md,go,js")

Examples:
  livemd start
  livemd add README.md
  livemd add docs/guide.md
  livemd add ./docs -r
  livemd add ./src -r --filter "md,go"
  livemd list
`, Version)
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "start":
		cmdStart()
	case "add":
		cmdAdd()
	case "remove":
		cmdRemove()
	case "list":
		cmdList()
	case "stop":
		cmdStop()
	case "port":
		cmdPort()
	case "version", "--version", "-v":
		fmt.Printf("livemd %s %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)
	case "update":
		cmdUpdate()
	case "--help", "-h", "help":
		flag.Usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}

// cmdStart handles the "livemd start" command.
// It launches the HTTP server on the specified port (default 3000).
// If the server is already running (detected via lock file), it exits with an error.
// The server runs in the foreground until stopped via "livemd stop" or SIGINT.
func cmdStart() {
	defaultPort := readConfigPort()
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	port := fs.Int("port", defaultPort, "port to serve on")
	fs.Parse(os.Args[2:])

	// Check if already running
	if lockPort, err := readLockFile(); err == nil {
		fmt.Printf("LiveMD already running on port %d\n", lockPort)
		printServerAddresses(lockPort)
		os.Exit(1)
	}

	// Auto-detect available port if the requested one is in use
	actualPort := *port
	if !isPortAvailable(actualPort) {
		originalPort := actualPort
		actualPort = findAvailablePort(actualPort)
		fmt.Printf("  Port %d is in use, using port %d instead\n", originalPort, actualPort)
	}

	// Write lock file
	if err := writeLockFile(actualPort); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing lock file: %v\n", err)
		os.Exit(1)
	}

	// Start server
	fmt.Printf("\n  LiveMD server started\n")
	printServerAddresses(actualPort)
	fmt.Println("  Use 'livemd add <file.md>' to watch files")
	fmt.Println("  Use 'livemd stop' to stop the server")
	fmt.Println()

	StartServer(actualPort)
}

// isPortAvailable checks if a TCP port can be listened on.
func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// findAvailablePort scans upward from startPort to find the next available port.
func findAvailablePort(startPort int) int {
	for p := startPort + 1; p <= startPort+100; p++ {
		if isPortAvailable(p) {
			return p
		}
	}
	// Fallback: let the OS pick
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return startPort
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// getNetworkAddresses returns all non-loopback IPv4 addresses from active network interfaces.
// This enables displaying accessible URLs for LAN devices to connect to the server.
// Loopback addresses (127.x.x.x) and IPv6 addresses are excluded from the results.
func getNetworkAddresses() []string {
	var addresses []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return addresses
	}

	for _, iface := range ifaces {
		// Skip down or loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Only include IPv4 addresses
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			addresses = append(addresses, ip.String())
		}
	}

	return addresses
}

// printServerAddresses prints localhost and all network interface addresses to stdout.
// This provides users with all URLs that can be used to access the server, including
// localhost for local access and LAN IPs for access from other devices on the network.
func printServerAddresses(port int) {
	fmt.Printf("  http://localhost:%d\n", port)

	networkAddrs := getNetworkAddresses()
	for _, addr := range networkAddrs {
		fmt.Printf("  http://%s:%d\n", addr, port)
	}
	fmt.Println()
}

// cmdAdd handles the "livemd add" command.
// It adds files or directories to the server's watch list via the HTTP API.
//
// Flags:
//   - -r, --recursive: Enable recursive directory scanning
//   - --filter: Comma-separated list of extensions to include (e.g., "md,go,js")
//
// The function handles both WSL/Windows path conversion and supports adding
// single files or entire directories with extension filtering.
func cmdAdd() {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	recursive := fs.Bool("r", false, "recursively add files from folder")
	recursiveLong := fs.Bool("recursive", false, "recursively add files from folder")
	filter := fs.String("filter", "", "filter by extensions (comma-separated, e.g. \"md,go,js\")")

	// Reorder args so flags come first (Go flag package stops at first positional arg)
	args := os.Args[2:]
	var flags []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			// Check if this flag takes a value
			if (arg == "--filter" || arg == "-filter") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, arg)
		}
	}

	reordered := append(flags, positional...)
	fs.Parse(reordered)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: livemd add <file|folder> [-r] [--filter EXT]")
		os.Exit(1)
	}

	pathArg := fs.Arg(0)
	isRecursive := *recursive || *recursiveLong

	// Try path conversion for WSL/Windows interop
	convertedPath := NormalizePath(pathArg)

	absPath, err := filepath.Abs(convertedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Try original path if converted doesn't exist
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		// Try the original path
		origAbs, _ := filepath.Abs(pathArg)
		if info2, err2 := os.Stat(origAbs); err2 == nil {
			absPath = origAbs
			info = info2
		} else {
			fmt.Fprintf(os.Stderr, "Path not found: %s\n", pathArg)
			if convertedPath != pathArg {
				fmt.Fprintf(os.Stderr, "  (tried: %s)\n", absPath)
			}
			os.Exit(1)
		}
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing path: %v\n", err)
		os.Exit(1)
	}

	port, err := readLockFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "LiveMD server not running. Start it with 'livemd start'")
		os.Exit(1)
	}

	// Handle directory
	if info.IsDir() {
		if !isRecursive {
			fmt.Fprintf(os.Stderr, "Error: %s is a directory. Use -r flag to add recursively.\n", pathArg)
			fmt.Fprintf(os.Stderr, "  Example: livemd add %s -r\n", pathArg)
			os.Exit(1)
		}
		addFolder(absPath, port, *filter)
		return
	}

	// Handle single file
	addSingleFile(absPath, port)
}

// addSingleFile sends a POST request to the server's /api/watch endpoint
// to add a single file to the watch list. It reports success or failure to stdout/stderr.
func addSingleFile(absPath string, port int) {
	body, _ := json.Marshal(map[string]string{"path": absPath})
	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/api/watch", port), "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(respBody))
		os.Exit(1)
	}

	fmt.Printf("Watching: %s\n", filepath.Base(absPath))
}

// addFolder recursively scans a directory and adds all matching files to the watch list.
// It filters files by extension using either defaultExtensions or a custom filter.
// Hidden directories (starting with ".") are skipped during traversal.
// If more than 500 files are found, it prompts for user confirmation before proceeding.
func addFolder(folderPath string, port int, filterExts string) {
	// Build extension filter
	allowedExts := defaultExtensions
	if filterExts != "" {
		allowedExts = []string{}
		for _, ext := range strings.Split(filterExts, ",") {
			ext = strings.TrimSpace(ext)
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			allowedExts = append(allowedExts, strings.ToLower(ext))
		}
	}

	// Collect all matching files
	var files []string
	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}
		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") && path != folderPath {
				return filepath.SkipDir
			}
			return nil
		}
		// Check extension
		ext := strings.ToLower(filepath.Ext(path))
		for _, allowed := range allowedExts {
			if ext == allowed {
				files = append(files, path)
				break
			}
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning folder: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Println("No supported files found in folder.")
		if filterExts != "" {
			fmt.Printf("  Filter: %s\n", filterExts)
		}
		return
	}

	// Warn about large folder
	const warnThreshold = 500
	if len(files) > warnThreshold {
		fmt.Printf("Warning: Found %d files. This may affect performance.\n", len(files))
		fmt.Print("Continue? [y/N] ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("Cancelled.")
			return
		}
	}

	fmt.Printf("Found %d files in %s\n", len(files), folderPath)

	// Add each file
	added := 0
	skipped := 0
	for _, file := range files {
		body, _ := json.Marshal(map[string]string{"path": file})
		resp, err := http.Post(fmt.Sprintf("http://localhost:%d/api/watch", port), "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error: %s - %v\n", filepath.Base(file), err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			added++
			fmt.Printf("  + %s\n", filepath.Base(file))
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			// Don't print "already watching" as an error
			if strings.Contains(string(respBody), "already watching") {
				skipped++
			} else {
				fmt.Fprintf(os.Stderr, "  ! %s: %s\n", filepath.Base(file), string(respBody))
			}
		}
		resp.Body.Close()
	}

	fmt.Printf("\nAdded %d file(s)", added)
	if skipped > 0 {
		fmt.Printf(" (%d already watched)", skipped)
	}
	fmt.Println()
}

// cmdRemove handles the "livemd remove" command.
// It sends a DELETE request to the server's /api/watch endpoint to stop watching a file.
// The file must be specified by its path, which will be resolved to an absolute path.
func cmdRemove() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: livemd remove <file.md>")
		os.Exit(1)
	}

	filePath := os.Args[2]
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	port, err := readLockFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "LiveMD server not running.")
		os.Exit(1)
	}

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://localhost:%d/api/watch?path=%s", port, absPath), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(respBody))
		os.Exit(1)
	}

	fmt.Printf("Stopped watching: %s\n", filepath.Base(absPath))
}

// cmdList handles the "livemd list" command.
// It retrieves and displays all currently watched files from the server's /api/files endpoint.
// For each file, it shows the filename, full path, tracking start time, and last change time.
func cmdList() {
	port, err := readLockFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "LiveMD server not running.")
		os.Exit(1)
	}

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/files", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var files []WatchedFile
	json.NewDecoder(resp.Body).Decode(&files)

	if len(files) == 0 {
		fmt.Println("No files being watched.")
		fmt.Println("Use 'livemd add <file.md>' to add files.")
		return
	}

	fmt.Printf("Watching %d file(s):\n\n", len(files))
	for _, f := range files {
		fmt.Printf("  %s\n", f.Name)
		fmt.Printf("    Path: %s\n", f.Path)
		fmt.Printf("    Tracking since: %s\n", f.TrackTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("    Last change: %s\n", f.LastChange.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}
}

// cmdStop handles the "livemd stop" command.
// It sends a POST request to the server's /api/shutdown endpoint to initiate graceful shutdown.
// The lock file is removed regardless of whether the server responds (it may have already exited).
func cmdStop() {
	port, err := readLockFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "LiveMD server not running.")
		os.Exit(1)
	}

	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/api/shutdown", port), "", nil)
	if err != nil {
		// Server might have already shut down
		removeLockFile()
		fmt.Println("LiveMD server stopped.")
		return
	}
	defer resp.Body.Close()

	removeLockFile()
	fmt.Println("LiveMD server stopped.")
}

// cmdPort handles the "livemd port" command.
// With no arguments, it displays the current configured port.
// With a port number argument, it sets the default port for future server starts.
func cmdPort() {
	if len(os.Args) < 3 {
		port := readConfigPort()
		fmt.Printf("Default port: %d\n", port)
		if lockPort, err := readLockFile(); err == nil {
			fmt.Printf("Running on:   %d\n", lockPort)
			printServerAddresses(lockPort)
		}
		return
	}

	portStr := os.Args[2]
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		fmt.Fprintf(os.Stderr, "Invalid port: %s (must be 1-65535)\n", portStr)
		os.Exit(1)
	}

	if err := writeConfigPort(port); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving port: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Default port set to %d\n", port)
}

// Config file helpers
//
// The config file stores user preferences like the default port.
// Location: ~/.livemd.conf (Unix) or %APPDATA%/livemd.conf (Windows)

func getConfigFilePath() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("USERPROFILE")
		}
		return filepath.Join(appData, "livemd.conf")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".livemd.conf")
}

func readConfigPort() int {
	data, err := os.ReadFile(getConfigFilePath())
	if err != nil {
		return 3000
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "port=") {
			if p, err := strconv.Atoi(strings.TrimPrefix(line, "port=")); err == nil && p > 0 && p <= 65535 {
				return p
			}
		}
	}
	return 3000
}

func writeConfigPort(port int) error {
	return os.WriteFile(getConfigFilePath(), []byte(fmt.Sprintf("port=%d\n", port)), 0644)
}

// Lock file helpers
//
// The lock file stores the server's port number and serves two purposes:
// 1. Prevents multiple server instances from running simultaneously
// 2. Allows CLI commands to discover and communicate with the running server
//
// Location: ~/.livemd.lock (Unix) or %APPDATA%/livemd.lock (Windows)

// getLockFilePath returns the platform-specific path for the lock file.
// On Windows, it uses APPDATA or USERPROFILE. On Unix systems, it uses the home directory.
func getLockFilePath() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("USERPROFILE")
		}
		return filepath.Join(appData, "livemd.lock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".livemd.lock")
}

// writeLockFile creates the lock file containing the server's port number.
// Called by cmdStart after verifying no existing server is running.
func writeLockFile(port int) error {
	return os.WriteFile(getLockFilePath(), []byte(strconv.Itoa(port)), 0644)
}

// readLockFile reads the port number from the lock file.
// Returns an error if the lock file doesn't exist (server not running) or is invalid.
func readLockFile() (int, error) {
	data, err := os.ReadFile(getLockFilePath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// removeLockFile deletes the lock file during server shutdown.
// Errors are silently ignored as the file may already be absent.
func removeLockFile() {
	os.Remove(getLockFilePath())
}
