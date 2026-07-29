package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static
var staticFiles embed.FS

// WatchedFile represents a file being watched
type WatchedFile struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	TrackTime  time.Time `json:"trackTime"`
	LastChange time.Time `json:"lastChange"`
	HTML       string    `json:"html,omitempty"`
	Active     bool      `json:"active"`  // true if actively being watched by fsnotify
	Deleted    bool      `json:"deleted"` // true if file was deleted from disk
}

// Message sent to clients via WebSocket
type Message struct {
	Type    string          `json:"type"`
	Files   []WatchedFile   `json:"files,omitempty"`
	Folders []WatchedFolder `json:"folders,omitempty"`
	File    *WatchedFile    `json:"file,omitempty"`
	Path    string          `json:"path,omitempty"`
	Log     *LogEntry       `json:"log,omitempty"`
	Logs    []LogEntry      `json:"logs,omitempty"`
}

// Client represents a connected WebSocket client
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub manages files, watchers, and WebSocket clients
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client

	mu        sync.RWMutex
	files     map[string]*WatchedFile
	watchers  map[string]*Watcher
	folders   map[string]*WatchedFolder
	folderMgr *FolderManager
	renderer  *Renderer
	logger    *Logger
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		files:      make(map[string]*WatchedFile),
		watchers:   make(map[string]*Watcher),
		folders:    make(map[string]*WatchedFolder),
		renderer:   NewRenderer(),
		logger:     NewLogger(100),
	}
	h.logger.SetHub(h)

	fm, err := NewFolderManager(h)
	if err != nil {
		h.logger.Warn(fmt.Sprintf("Could not create folder watcher: %v", err))
	} else {
		h.folderMgr = fm
	}

	h.restoreFromState()
	return h
}

// persistState writes the current files+folders to disk so they survive restart.
// Called from any add/remove/toggle path; safe to call from inside a Hub method
// that is NOT already holding h.mu (it acquires its own RLock).
func (h *Hub) persistState() {
	h.mu.RLock()
	files := make([]StateFile, 0, len(h.files))
	for _, f := range h.files {
		files = append(files, StateFile{Path: f.Path, Active: false}) // active is session-scoped
	}
	folders := make([]WatchedFolder, 0, len(h.folders))
	for _, f := range h.folders {
		folders = append(folders, *f)
	}
	h.mu.RUnlock()

	if err := saveState(&State{Files: files, Folders: folders}); err != nil {
		h.logger.Warn(fmt.Sprintf("Persist state: %v", err))
	}
}

// restoreFromState loads the saved state file, registering files and following
// folders. Missing files are silently skipped (file may have been deleted while
// the daemon was down).
func (h *Hub) restoreFromState() {
	state, err := loadState()
	if err != nil {
		h.logger.Warn(fmt.Sprintf("Load state: %v", err))
		return
	}

	for _, sf := range state.Files {
		if _, err := os.Stat(sf.Path); err != nil {
			continue // file gone, skip silently
		}
		if err := h.AddFile(sf.Path); err != nil {
			h.logger.Warn(fmt.Sprintf("Restore file %s: %v", sf.Path, err))
		}
	}

	if h.folderMgr == nil {
		return
	}
	for i := range state.Folders {
		folder := state.Folders[i] // copy
		if _, err := os.Stat(folder.Path); err != nil {
			continue
		}
		files, err := h.folderMgr.Follow(&folder)
		if err != nil {
			h.logger.Warn(fmt.Sprintf("Restore folder %s: %v", folder.Path, err))
			continue
		}
		h.mu.Lock()
		h.folders[folder.Path] = &folder
		h.mu.Unlock()
		for _, f := range files {
			h.AddFile(f) // ignore already-registered errors
		}
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			h.logger.Info("Browser connected")
			// Send current file list to new client
			h.sendFileList(client)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.logger.Info("Browser disconnected")
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// snapshotFilesFolders captures the current file + folder lists under the lock.
func (h *Hub) snapshotFilesFolders() ([]WatchedFile, []WatchedFolder) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	files := make([]WatchedFile, 0, len(h.files))
	for _, f := range h.files {
		files = append(files, *f)
	}
	folders := make([]WatchedFolder, 0, len(h.folders))
	for _, f := range h.folders {
		folders = append(folders, *f)
	}
	return files, folders
}

func (h *Hub) sendFileList(client *Client) {
	files, folders := h.snapshotFilesFolders()
	msg := Message{Type: "files", Files: files, Folders: folders}
	data, _ := json.Marshal(msg)
	client.send <- data

	logs := h.logger.GetEntries()
	logsMsg := Message{Type: "logs", Logs: logs}
	logsData, _ := json.Marshal(logsMsg)
	client.send <- logsData
}

func (h *Hub) broadcastFileList() {
	files, folders := h.snapshotFilesFolders()
	msg := Message{Type: "files", Files: files, Folders: folders}
	data, _ := json.Marshal(msg)
	h.broadcast <- data
}

func (h *Hub) broadcastFileUpdate(file *WatchedFile) {
	msg := Message{Type: "update", File: file}
	data, _ := json.Marshal(msg)
	h.broadcast <- data
}

func (h *Hub) broadcastLog(entry LogEntry) {
	msg := Message{Type: "log", Log: &entry}
	data, _ := json.Marshal(msg)
	h.broadcast <- data
}

// FollowFolder registers a folder, runs initial discovery, and starts watching
// it for new files. Live defaults to true (the user opted into "follow"; checkbox
// can flip it later). Idempotent: re-following an existing folder returns nil.
func (h *Hub) FollowFolder(folder *WatchedFolder) error {
	if h.folderMgr == nil {
		return fmt.Errorf("folder watcher unavailable")
	}

	h.mu.Lock()
	for existing := range h.folders {
		if PathsEqual(existing, folder.Path) {
			h.mu.Unlock()
			return nil
		}
	}
	h.folders[folder.Path] = folder
	h.mu.Unlock()

	files, err := h.folderMgr.Follow(folder)
	if err != nil {
		h.mu.Lock()
		delete(h.folders, folder.Path)
		h.mu.Unlock()
		return err
	}

	for _, f := range files {
		_ = h.AddFile(f) // ignore "already registered"
	}

	h.logger.Info(fmt.Sprintf("Following folder: %s (%d files)", folder.Path, len(files)))
	h.broadcastFileList()
	h.persistState()
	return nil
}

// UnfollowFolder stops auto-adding new files for a folder. Existing watched
// files stay registered (use RemoveFolder to also drop them).
func (h *Hub) UnfollowFolder(path string) error {
	h.mu.Lock()
	var actual string
	for existing := range h.folders {
		if PathsEqual(existing, path) {
			actual = existing
			break
		}
	}
	if actual == "" {
		h.mu.Unlock()
		return fmt.Errorf("folder not followed: %s", path)
	}
	delete(h.folders, actual)
	h.mu.Unlock()

	if h.folderMgr != nil {
		h.folderMgr.Unfollow(actual)
	}
	h.logger.Info(fmt.Sprintf("Unfollowed folder: %s", actual))
	h.broadcastFileList()
	h.persistState()
	return nil
}

// SetFolderLive toggles whether new files in the folder are auto-added.
func (h *Hub) SetFolderLive(path string, live bool) error {
	h.mu.Lock()
	var actual string
	var folder *WatchedFolder
	for existing, f := range h.folders {
		if PathsEqual(existing, path) {
			actual = existing
			folder = f
			break
		}
	}
	if folder == nil {
		h.mu.Unlock()
		return fmt.Errorf("folder not followed: %s", path)
	}
	folder.Live = live
	h.mu.Unlock()

	if h.folderMgr != nil {
		h.folderMgr.SetLive(actual, live)
	}
	h.broadcastFileList()
	h.persistState()
	return nil
}

func (h *Hub) AddFile(path string) error {
	return h.AddFileWithActive(path, false)
}

func (h *Hub) AddFileWithActive(path string, active bool) error {
	h.mu.Lock()

	// Check if already registered (case-insensitive on Windows)
	for existingPath := range h.files {
		if PathsEqual(existingPath, path) {
			h.mu.Unlock()
			return fmt.Errorf("already registered: %s", filepath.Base(existingPath))
		}
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		h.mu.Unlock()
		return err
	}

	// Refuse oversized files at the door so they never clutter the list.
	// Media is exempt: the browser streams it from /raw, so size costs the
	// daemon nothing. Folder discovery drops the error, hence the log line.
	if !isStreamedMedia(path) && info.Size() > maxFileSize {
		h.mu.Unlock()
		msg := fmt.Sprintf("%s is %.1f MB, over the %d MB limit",
			filepath.Base(path), float64(info.Size())/(1<<20), maxFileSize>>20)
		h.logger.Warn("Skipped: " + msg)
		return fmt.Errorf("%s", msg)
	}

	// Render content
	html, err := h.renderer.Render(path)
	if err != nil {
		h.mu.Unlock()
		return err
	}

	file := &WatchedFile{
		Path:       path,
		Name:       filepath.Base(path),
		TrackTime:  time.Now(),
		LastChange: info.ModTime(),
		HTML:       html,
		Active:     active,
	}
	h.files[path] = file

	h.mu.Unlock()

	// Only start watcher if active
	if active {
		h.startWatcher(path)
		h.logger.Info(fmt.Sprintf("Started watching: %s", filepath.Base(path)))
	} else {
		h.logger.Info(fmt.Sprintf("Registered: %s", filepath.Base(path)))
	}

	h.broadcastFileList()
	h.persistState()
	return nil
}

func (h *Hub) startWatcher(path string) {
	h.mu.Lock()
	// Check if watcher already exists
	if _, exists := h.watchers[path]; exists {
		h.mu.Unlock()
		return
	}

	watcher := NewWatcher()
	h.watchers[path] = watcher
	h.mu.Unlock()

	// Watch for changes
	watcher.Watch(path, func() {
		h.mu.Lock()
		f, exists := h.files[path]
		if !exists || !f.Active {
			h.mu.Unlock()
			return
		}

		html, err := h.renderer.Render(path)
		if err != nil {
			h.logger.Error(fmt.Sprintf("Error rendering %s: %v", filepath.Base(path), err))
			h.mu.Unlock()
			return
		}

		info, _ := os.Stat(path)
		f.HTML = html
		f.LastChange = info.ModTime()
		f.Deleted = false // file is back if it was marked deleted
		h.mu.Unlock()

		h.logger.Info(fmt.Sprintf("File changed: %s", filepath.Base(path)))
		h.broadcastFileUpdate(f)
	}, func() {
		// onDelete callback
		h.mu.Lock()
		f, exists := h.files[path]
		if !exists {
			h.mu.Unlock()
			return
		}
		f.Deleted = true
		f.Active = false
		h.mu.Unlock()

		h.logger.Warn(fmt.Sprintf("File deleted: %s", filepath.Base(path)))
		h.broadcastFileList()
	})
}

func (h *Hub) ActivateFile(path string) error {
	h.mu.Lock()

	// Find the file (case-insensitive on Windows)
	var actualPath string
	var file *WatchedFile
	for existingPath, f := range h.files {
		if PathsEqual(existingPath, path) {
			actualPath = existingPath
			file = f
			break
		}
	}

	if file == nil {
		h.mu.Unlock()
		return fmt.Errorf("file not registered: %s", path)
	}

	if file.Active {
		h.mu.Unlock()
		return nil // Already active
	}

	// Refresh content before activating
	html, err := h.renderer.Render(actualPath)
	if err != nil {
		h.mu.Unlock()
		return err
	}

	info, _ := os.Stat(actualPath)
	file.HTML = html
	file.LastChange = info.ModTime()
	file.Active = true
	h.mu.Unlock()

	// Start watching
	h.startWatcher(actualPath)

	h.logger.Info(fmt.Sprintf("Activated watching: %s", filepath.Base(actualPath)))
	h.broadcastFileList()
	return nil
}

func (h *Hub) DeactivateFile(path string) error {
	h.mu.Lock()

	// Find the file (case-insensitive on Windows)
	var actualPath string
	var file *WatchedFile
	for existingPath, f := range h.files {
		if PathsEqual(existingPath, path) {
			actualPath = existingPath
			file = f
			break
		}
	}

	if file == nil {
		h.mu.Unlock()
		return fmt.Errorf("file not registered: %s", path)
	}

	if !file.Active {
		h.mu.Unlock()
		return nil // Already inactive
	}

	file.Active = false

	// Stop watcher
	if w, exists := h.watchers[actualPath]; exists {
		w.Close()
		delete(h.watchers, actualPath)
	}

	h.mu.Unlock()

	h.logger.Info(fmt.Sprintf("Deactivated watching: %s", filepath.Base(actualPath)))
	h.broadcastFileList()
	return nil
}

func (h *Hub) RemoveFile(path string) error {
	h.mu.Lock()

	// Find the actual key (case-insensitive on Windows)
	var actualPath string
	var file *WatchedFile
	for existingPath, f := range h.files {
		if PathsEqual(existingPath, path) {
			actualPath = existingPath
			file = f
			break
		}
	}

	if file == nil {
		h.mu.Unlock()
		return fmt.Errorf("not watching: %s", path)
	}
	name := file.Name

	// Stop watcher
	if w, exists := h.watchers[actualPath]; exists {
		w.Close()
		delete(h.watchers, actualPath)
	}

	delete(h.files, actualPath)
	h.mu.Unlock()

	h.logger.Info(fmt.Sprintf("Stopped watching: %s", name))

	// Broadcast removal
	msg := Message{Type: "removed", Path: actualPath}
	data, _ := json.Marshal(msg)
	h.broadcast <- data

	h.persistState()
	return nil
}

func (h *Hub) RemoveFolder(folderPath string) int {
	h.mu.Lock()
	var toRemove []string
	prefix := folderPath + "/"
	for path := range h.files {
		if strings.HasPrefix(path, prefix) {
			toRemove = append(toRemove, path)
		}
	}
	for _, path := range toRemove {
		if w, exists := h.watchers[path]; exists {
			w.Close()
			delete(h.watchers, path)
		}
		delete(h.files, path)
	}
	// If this matches a followed folder root, also unfollow so we stop auto-adding.
	var unfollow string
	for existing := range h.folders {
		if PathsEqual(existing, folderPath) {
			unfollow = existing
			break
		}
	}
	if unfollow != "" {
		delete(h.folders, unfollow)
	}
	h.mu.Unlock()

	if unfollow != "" && h.folderMgr != nil {
		h.folderMgr.Unfollow(unfollow)
	}

	if len(toRemove) > 0 {
		h.logger.Info(fmt.Sprintf("Removed %d file(s) from folder: %s", len(toRemove), filepath.Base(folderPath)))
		h.broadcastFileList()
		h.persistState()
	}
	return len(toRemove)
}

func (h *Hub) RemoveDeletedFiles() int {
	h.mu.Lock()
	var toRemove []string
	for path, f := range h.files {
		if f.Deleted {
			toRemove = append(toRemove, path)
		}
	}
	for _, path := range toRemove {
		if w, exists := h.watchers[path]; exists {
			w.Close()
			delete(h.watchers, path)
		}
		delete(h.files, path)
	}
	h.mu.Unlock()

	if len(toRemove) > 0 {
		h.logger.Info(fmt.Sprintf("Removed %d deleted file(s)", len(toRemove)))
		h.broadcastFileList()
		h.persistState()
	}
	return len(toRemove)
}

func (h *Hub) GetFiles() []WatchedFile {
	h.mu.RLock()
	defer h.mu.RUnlock()

	files := make([]WatchedFile, 0, len(h.files))
	for _, f := range h.files {
		files = append(files, *f)
	}
	return files
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, w := range h.watchers {
		w.Close()
	}
}

// Server handles HTTP and WebSocket
type Server struct {
	hub    *Hub
	port   int
	server *http.Server
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	s.hub.register <- client

	// Writer goroutine
	go func() {
		defer conn.Close()
		for message := range client.send {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		}
	}()

	// Reader goroutine (detect disconnect)
	go func() {
		defer func() {
			s.hub.unregister <- client
			conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()
}

// handleRaw serves the raw bytes of a watched file. The query parameter `path`
// must match (case-insensitively on Windows) a path in Hub.files; any other
// path is treated as not-found, blocking arbitrary disk access.
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requested := r.URL.Query().Get("path")
	if requested == "" {
		http.Error(w, "Missing path", http.StatusBadRequest)
		return
	}

	// Allowlist lookup: only serve files that are registered in the watch list.
	s.hub.mu.RLock()
	var actual string
	for k := range s.hub.files {
		if PathsEqual(k, requested) {
			actual = k
			break
		}
	}
	s.hub.mu.RUnlock()
	if actual == "" {
		http.NotFound(w, r)
		return
	}

	// http.ServeFile handles range requests (important for video seeking) and
	// sets Content-Type from the extension.
	http.ServeFile(w, r, actual)
}

// handleRender re-renders a watched file in a caller-chosen view
// (mode=raw forces the source view). Backs the Preview/Raw toggle.
// Same allowlist as /raw: only paths already in the watch list.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	requested := r.URL.Query().Get("path")
	if requested == "" {
		http.Error(w, "Missing path", http.StatusBadRequest)
		return
	}
	mode := modeAuto
	if r.URL.Query().Get("mode") == "raw" {
		mode = modeRaw
	}

	s.hub.mu.RLock()
	var actual string
	for k := range s.hub.files {
		if PathsEqual(k, requested) {
			actual = k
			break
		}
	}
	s.hub.mu.RUnlock()
	if actual == "" {
		http.NotFound(w, r)
		return
	}

	html, err := s.hub.renderer.RenderMode(actual, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"html": html})
}

func (s *Server) handleAddFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string `json:"path"`
		Active bool   `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := s.hub.AddFileWithActive(req.Path, req.Active); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleActivateFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	if err := s.hub.ActivateFile(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeactivateFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	if err := s.hub.DeactivateFile(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRemoveFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	if err := s.hub.RemoveFile(path); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	files := s.hub.GetFiles()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.hub.logger.GetEntries()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	releases, err := fetchAllReleases()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(releases)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	info := CheckForUpdate()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func StartServer(port int) {
	hub := NewHub()
	go hub.Run()

	// Restore previously watched files
	

	s := &Server{
		hub:  hub,
		port: port,
	}

	mux := http.NewServeMux()

	// Serve index.html at root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := staticFiles.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// Serve static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// API endpoints
	// Raw file content for non-text viewers (images, PDFs, audio, video).
	// Allowlisted to paths in the watch list — rejects everything else.
	mux.HandleFunc("/raw", s.handleRaw)

	mux.HandleFunc("/api/watch", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			s.handleAddFile(w, r)
		case http.MethodDelete:
			s.handleRemoveFile(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/files", s.handleListFiles)
	mux.HandleFunc("/api/files/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleActivateFile(w, r)
	})
	mux.HandleFunc("/api/files/deactivate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleDeactivateFile(w, r)
	})
	// Followed-folder management. POST follows, DELETE unfollows, /toggle-live
	// flips the auto-add bit.
	mux.HandleFunc("/api/folders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var req WatchedFolder
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}
			req.Live = true
			if err := s.hub.FollowFolder(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			path := r.URL.Query().Get("path")
			if path == "" {
				http.Error(w, "Missing path", http.StatusBadRequest)
				return
			}
			if err := s.hub.UnfollowFolder(path); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/folders/toggle-live", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Path string `json:"path"`
			Live bool   `json:"live"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if err := s.hub.SetFolderLive(req.Path, req.Live); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/files/remove-folder", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path parameter", http.StatusBadRequest)
			return
		}
		count := s.hub.RemoveFolder(path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"removed": count})
	})
	mux.HandleFunc("/api/files/remove-deleted", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		count := s.hub.RemoveDeletedFiles()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"removed": count})
	})
	mux.HandleFunc("/api/render", s.handleRender)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/releases", s.handleReleases)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRemoveFile(w, r)
	})
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() {
			time.Sleep(100 * time.Millisecond)
			hub.Close()
			s.server.Shutdown(context.Background())
		}()
	})

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Check for updates in background on startup
	go func() {
		time.Sleep(2 * time.Second) // Wait for server to be ready
		if Version != "dev" {
			info := CheckForUpdate()
			if info.UpdateAvail {
				hub.logger.Info(fmt.Sprintf("Update available: %s (current: %s)", info.Latest, info.Current))
			}
		}
	}()

	// Graceful shutdown on signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		hub.Close()
		removeLockFile()
		s.server.Shutdown(context.Background())
	}()

	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
