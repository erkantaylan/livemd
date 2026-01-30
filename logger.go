package main

import (
	"sync"
	"time"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"` // info, warn, error
	Message string    `json:"message"`
}

// Logger stores log entries and broadcasts to clients
type Logger struct {
	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
	hub     *Hub
}

func NewLogger(maxSize int) *Logger {
	return &Logger{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

func (l *Logger) SetHub(hub *Hub) {
	l.hub = hub
}

func (l *Logger) add(level, message string) {
	l.mu.Lock()
	entry := LogEntry{
		Time:    time.Now(),
		Level:   level,
		Message: message,
	}
	l.entries = append(l.entries, entry)
	if len(l.entries) > l.maxSize {
		l.entries = l.entries[1:]
	}
	l.mu.Unlock()

	// Broadcast to clients
	if l.hub != nil {
		l.hub.broadcastLog(entry)
	}
}

func (l *Logger) Info(message string) {
	l.add("info", message)
}

func (l *Logger) Warn(message string) {
	l.add("warn", message)
}

func (l *Logger) Error(message string) {
	l.add("error", message)
}

func (l *Logger) GetEntries() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	entries := make([]LogEntry, len(l.entries))
	copy(entries, l.entries)
	return entries
}
