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
}

// Message sent to clients via WebSocket
type Message struct {
	Type  string        `json:"type"`
	Files []WatchedFile `json:"files,omitempty"`
	File  *WatchedFile  `json:"file,omitempty"`
	Path  string        `json:"path,omitempty"`
	Log   *LogEntry     `json:"log,omitempty"`
	Logs  []LogEntry    `json:"logs,omitempty"`
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

	mu       sync.RWMutex
	files    map[string]*WatchedFile
	watchers map[string]*Watcher
	renderer *Renderer
	logger   *Logger
}

func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		files:      make(map[string]*WatchedFile),
		watchers:   make(map[string]*Watcher),
		renderer:   NewRenderer(),
		logger:     NewLogger(100),
	}
	h.logger.SetHub(h)
	return h
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

func (h *Hub) sendFileList(client *Client) {
	h.mu.RLock()
	files := make([]WatchedFile, 0, len(h.files))
	for _, f := range h.files {
		files = append(files, *f)
	}
	h.mu.RUnlock()

	msg := Message{Type: "files", Files: files}
	data, _ := json.Marshal(msg)
	client.send <- data

	// Also send logs
	logs := h.logger.GetEntries()
	logsMsg := Message{Type: "logs", Logs: logs}
	logsData, _ := json.Marshal(logsMsg)
	client.send <- logsData
}

func (h *Hub) broadcastFileList() {
	h.mu.RLock()
	files := make([]WatchedFile, 0, len(h.files))
	for _, f := range h.files {
		files = append(files, *f)
	}
	h.mu.RUnlock()

	msg := Message{Type: "files", Files: files}
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

func (h *Hub) AddFile(path string) error {
	h.mu.Lock()

	// Check if already watching
	if _, exists := h.files[path]; exists {
		h.mu.Unlock()
		return fmt.Errorf("already watching: %s", path)
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		h.mu.Unlock()
		return err
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
	}
	h.files[path] = file

	// Start watcher
	watcher := NewWatcher()
	h.watchers[path] = watcher

	h.mu.Unlock()

	// Watch for changes
	watcher.Watch(path, func() {
		h.mu.Lock()
		f, exists := h.files[path]
		if !exists {
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
		h.mu.Unlock()

		h.logger.Info(fmt.Sprintf("File changed: %s", filepath.Base(path)))
		h.broadcastFileUpdate(f)
	})

	h.logger.Info(fmt.Sprintf("Started watching: %s", filepath.Base(path)))
	h.broadcastFileList()
	return nil
}

func (h *Hub) RemoveFile(path string) error {
	h.mu.Lock()

	file, exists := h.files[path]
	if !exists {
		h.mu.Unlock()
		return fmt.Errorf("not watching: %s", path)
	}
	name := file.Name

	// Stop watcher
	if w, exists := h.watchers[path]; exists {
		w.Close()
		delete(h.watchers, path)
	}

	delete(h.files, path)
	h.mu.Unlock()

	h.logger.Info(fmt.Sprintf("Stopped watching: %s", name))

	// Broadcast removal
	msg := Message{Type: "removed", Path: path}
	data, _ := json.Marshal(msg)
	h.broadcast <- data

	return nil
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

func (s *Server) handleAddFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := s.hub.AddFile(req.Path); err != nil {
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

func StartServer(port int) {
	hub := NewHub()
	go hub.Run()

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
	mux.HandleFunc("/api/logs", s.handleLogs)
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
