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

// Supported file extensions for watching
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

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `LiveMD - Live markdown viewer

Usage:
  livemd start [--port PORT]    Start the server
  livemd add <file.md>          Add file to watch
  livemd add <folder> -r        Add folder recursively
  livemd remove <file.md>       Remove file from watch
  livemd list                   List watched files
  livemd stop                   Stop the server

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
