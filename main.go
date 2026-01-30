package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `LiveMD - Live markdown viewer

Usage:
  livemd start [--port PORT]    Start the server
  livemd add <file.md>          Add file to watch
  livemd remove <file.md>       Remove file from watch
  livemd list                   List watched files
  livemd stop                   Stop the server

Options:
  --port PORT    Port to serve on (default 3000)

Examples:
  livemd start
  livemd add README.md
  livemd add docs/guide.md
  livemd list
`)
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
	case "--help", "-h", "help":
		flag.Usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}

func cmdStart() {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	port := fs.Int("port", 3000, "port to serve on")
	fs.Parse(os.Args[2:])

	// Check if already running
	if lockPort, err := readLockFile(); err == nil {
		fmt.Printf("LiveMD already running on port %d\n", lockPort)
		fmt.Printf("  http://localhost:%d\n", lockPort)
		os.Exit(1)
	}

	// Write lock file
	if err := writeLockFile(*port); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing lock file: %v\n", err)
		os.Exit(1)
	}

	// Start server
	fmt.Printf("\n  LiveMD server started\n")
	fmt.Printf("  http://localhost:%d\n\n", *port)
	fmt.Println("  Use 'livemd add <file.md>' to watch files")
	fmt.Println("  Use 'livemd stop' to stop the server")
	fmt.Println()

	StartServer(*port)
}

func cmdAdd() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: livemd add <file.md>")
		os.Exit(1)
	}

	filePath := os.Args[2]

	// Try path conversion for WSL/Windows interop
	convertedPath := NormalizePath(filePath)

	absPath, err := filepath.Abs(convertedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Try original path if converted doesn't exist
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		// Try the original path
		origAbs, _ := filepath.Abs(filePath)
		if _, err2 := os.Stat(origAbs); err2 == nil {
			absPath = origAbs
		} else {
			fmt.Fprintf(os.Stderr, "File not found: %s\n", filePath)
			if convertedPath != filePath {
				fmt.Fprintf(os.Stderr, "  (tried: %s)\n", absPath)
			}
			os.Exit(1)
		}
	}

	port, err := readLockFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "LiveMD server not running. Start it with 'livemd start'")
		os.Exit(1)
	}

	// Send request to server
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

// Lock file helpers

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

func writeLockFile(port int) error {
	return os.WriteFile(getLockFilePath(), []byte(strconv.Itoa(port)), 0644)
}

func readLockFile() (int, error) {
	data, err := os.ReadFile(getLockFilePath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func removeLockFile() {
	os.Remove(getLockFilePath())
}
