package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// State is the on-disk shape of the daemon's tracked files and followed folders.
// It survives `livemd stop` / `livemd start` so users don't have to re-add things
// every session.
type State struct {
	Files   []StateFile     `json:"files"`
	Folders []WatchedFolder `json:"folders"`
}

// StateFile is the persisted form of a tracked file.
// We don't persist HTML/timestamps — those are recomputed on load.
type StateFile struct {
	Path   string `json:"path"`
	Active bool   `json:"active"`
}

// getStateFilePath mirrors getConfigFilePath / getLockFilePath conventions.
func getStateFilePath() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("USERPROFILE")
		}
		return filepath.Join(appData, "livemd-state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".livemd-state.json")
}

func loadState() (*State, error) {
	path := getStateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		// The daemon starts empty after this, and its next save would replace
		// the unreadable file — a hand edit with one typo would erase every
		// watch. Move it aside so it can be fixed and restored.
		backup := path + ".corrupt-" + time.Now().Format("20060102-150405")
		if rerr := os.Rename(path, backup); rerr != nil {
			return nil, fmt.Errorf("parse state: %w (could not set it aside: %v)", err, rerr)
		}
		return nil, fmt.Errorf("parse state: %w (moved to %s)", err, backup)
	}
	return &s, nil
}

func saveState(s *State) error {
	path := getStateFilePath()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Atomic-ish write: temp file + rename so a crash mid-write doesn't corrupt.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
