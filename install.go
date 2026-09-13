package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const githubRepo = "erkantaylan/livemd"

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Assets      []githubAsset `json:"assets"`
}

// VersionInfo represents the current and latest version for the API
type VersionInfo struct {
	Current       string `json:"current"`
	Latest        string `json:"latest"`
	UpdateAvail   bool   `json:"updateAvailable"`
	LatestURL     string `json:"latestUrl,omitempty"`
	CheckedAt     string `json:"checkedAt"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// cmdInstall fetches the latest release from GitHub, replaces the running
// binary with it, ensures PATH, and restarts the daemon if it was running.
// Idempotent: when already at the latest version it still re-runs PATH and
// daemon checks.
func cmdInstall() {
	if Version == "dev" {
		fmt.Fprintln(os.Stderr, "Cannot self-install a dev build. Use 'make install' from source, or run the install script.")
		os.Exit(1)
	}

	fmt.Println("Checking GitHub for the latest release...")
	release, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !isNewer(Version, release.TagName) {
		fmt.Printf("Already at latest version (%s)\n", Version)
		cmdEnsurePath()
		return
	}

	fmt.Printf("Updating %s -> %s\n", Version, release.TagName)

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
		fmt.Fprintf(os.Stderr, "No release binary published for %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(1)
	}

	fmt.Printf("Downloading %s...\n", assetName)
	binary, err := downloadAsset(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading: %v\n", err)
		os.Exit(1)
	}

	// Stopping a systemd-run daemon over HTTP exits it cleanly, which
	// Restart=on-failure leaves stopped; stop and start it through systemd.
	managed := serviceManaged()
	wasRunning := false
	if managed && serviceActive() {
		if err := serviceStop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		wasRunning = true
	}
	if stopRunningDaemon() {
		wasRunning = true
	}
	if wasRunning {
		fmt.Println("Stopped running daemon.")
	}

	if err := replaceBinary(binary); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Installed %s\n", release.TagName)

	cmdEnsurePath()

	if wasRunning && managed {
		if err := serviceStart(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not restart the service: %v\n", err)
			return
		}
		fmt.Println("Restarted the livemd service.")
		return
	}
	if wasRunning {
		if err := relaunchDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not restart daemon: %v\n", err)
			fmt.Fprintln(os.Stderr, "Run 'livemd start --detach' manually.")
			return
		}
	}
}

// stopRunningDaemon issues shutdown to the running daemon and waits for the
// port to actually close so the .exe is unlocked on Windows. Returns true if
// a lockfile was present at the start (i.e., something looked like a daemon).
func stopRunningDaemon() bool {
	port, err := readLockFile()
	if err != nil {
		return false
	}
	resp, _ := http.Post(fmt.Sprintf("http://localhost:%d/api/shutdown", port), "", nil)
	if resp != nil {
		resp.Body.Close()
	}
	addr := fmt.Sprintf("localhost:%d", port)
	for i := 0; i < 60; i++ {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			break
		}
		c.Close()
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	removeLockFile()
	return true
}

// relaunchDaemon spawns `livemd start --detach` using the (possibly just-replaced) binary at os.Executable().
func relaunchDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	proc, err := os.StartProcess(exe, []string{exe, "start", "--detach"}, &os.ProcAttr{
		Files: []*os.File{nil, os.Stdout, os.Stderr},
	})
	if err != nil {
		return err
	}
	state, err := proc.Wait()
	if err != nil {
		return err
	}
	if !state.Success() {
		return fmt.Errorf("start --detach exited %d", state.ExitCode())
	}
	return nil
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

// fetchAllReleases returns all GitHub releases (for changelog display).
func fetchAllReleases() ([]githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases", githubRepo)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// CheckForUpdate checks if a newer version is available and returns version info.
func CheckForUpdate() VersionInfo {
	info := VersionInfo{
		Current:   Version,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}

	release, err := fetchLatestRelease()
	if err != nil {
		info.Latest = Version
		return info
	}

	info.Latest = release.TagName
	info.LatestURL = release.HTMLURL
	info.UpdateAvail = isNewer(Version, release.TagName)
	return info
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
