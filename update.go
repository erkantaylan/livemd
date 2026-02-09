package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const githubRepo = "erkantaylan/livemd"

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// cmdUpdate checks GitHub for a newer release and self-updates the binary.
func cmdUpdate() {
	if Version == "dev" {
		fmt.Fprintln(os.Stderr, "Cannot update a dev build. Install a release version first.")
		os.Exit(1)
	}

	fmt.Println("Checking for updates...")

	release, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
		os.Exit(1)
	}

	if !isNewer(Version, release.TagName) {
		fmt.Printf("Already up to date (%s)\n", Version)
		return
	}

	fmt.Printf("New version available: %s (current: %s)\n", release.TagName, Version)

	assetName := fmt.Sprintf("livemd-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}

	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "No release binary found for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(1)
	}

	fmt.Printf("Downloading %s...\n", assetName)

	binary, err := downloadAsset(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading update: %v\n", err)
		os.Exit(1)
	}

	if err := replaceBinary(binary); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing update: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Updated to %s\n", release.TagName)
}

func fetchLatestRelease() (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// isNewer returns true if remote version is newer than local.
// Both are expected to be semver tags like "v1.2.3".
func isNewer(local, remote string) bool {
	local = strings.TrimPrefix(local, "v")
	remote = strings.TrimPrefix(remote, "v")
	return remote != local && compareSemver(remote, local) > 0
}

// compareSemver compares two semver strings (without "v" prefix).
// Returns >0 if a > b, <0 if a < b, 0 if equal.
func compareSemver(a, b string) int {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)

	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bv)
		}
		if av != bv {
			return av - bv
		}
	}
	return 0
}

func downloadAsset(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// replaceBinary replaces the currently running binary with new content.
func replaceBinary(newBinary []byte) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("cannot resolve symlinks: %w", err)
	}

	if runtime.GOOS == "windows" {
		return replaceWindows(execPath, newBinary)
	}
	return replaceUnix(execPath, newBinary)
}

// replaceUnix writes to a temp file in the same dir then renames atomically.
func replaceUnix(execPath string, newBinary []byte) error {
	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, "livemd-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// replaceWindows renames the current exe to .bak, writes the new one in place.
func replaceWindows(execPath string, newBinary []byte) error {
	bakPath := execPath + ".bak"
	os.Remove(bakPath) // clean up previous backup

	if err := os.Rename(execPath, bakPath); err != nil {
		return fmt.Errorf("cannot rename old binary: %w", err)
	}

	if err := os.WriteFile(execPath, newBinary, 0755); err != nil {
		// Try to restore backup
		os.Rename(bakPath, execPath)
		return fmt.Errorf("cannot write new binary: %w", err)
	}

	return nil
}
