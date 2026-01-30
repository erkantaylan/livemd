package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ConvertPath attempts to convert between WSL and Windows paths
func ConvertPath(path string) string {
	if runtime.GOOS == "windows" {
		return convertToWindowsPath(path)
	}
	return convertToLinuxPath(path)
}

// convertToWindowsPath converts a Linux/WSL path to Windows path
func convertToWindowsPath(path string) string {
	// Already a Windows path
	if len(path) >= 2 && path[1] == ':' {
		return path
	}

	// Check if it's a /mnt/X/ path (WSL mounted Windows drive)
	if strings.HasPrefix(path, "/mnt/") && len(path) > 5 {
		drive := strings.ToUpper(string(path[5]))
		rest := ""
		if len(path) > 6 {
			rest = path[6:]
		}
		return drive + ":" + strings.ReplaceAll(rest, "/", "\\")
	}

	// It's a native WSL path, convert to \\wsl$\ or \\wsl.localhost\
	distro := getWSLDistro()
	if distro != "" {
		// Try \\wsl.localhost\ first (newer), fall back to \\wsl$\
		wslPath := `\\wsl.localhost\` + distro + path
		if _, err := os.Stat(wslPath); err == nil {
			return wslPath
		}
		wslPath = `\\wsl$\` + distro + path
		if _, err := os.Stat(wslPath); err == nil {
			return wslPath
		}
	}

	// Try common distro names
	distros := []string{"Ubuntu", "Ubuntu-22.04", "Ubuntu-20.04", "Debian", "kali-linux", "openSUSE-Leap-15", "Alpine"}
	for _, d := range distros {
		wslPath := `\\wsl.localhost\` + d + path
		if _, err := os.Stat(wslPath); err == nil {
			return wslPath
		}
		wslPath = `\\wsl$\` + d + path
		if _, err := os.Stat(wslPath); err == nil {
			return wslPath
		}
	}

	return path
}

// convertToLinuxPath converts a Windows path to Linux/WSL path
func convertToLinuxPath(path string) string {
	// Already a Linux path
	if strings.HasPrefix(path, "/") {
		return path
	}

	// Handle \\wsl$\ or \\wsl.localhost\ paths
	wslPattern := regexp.MustCompile(`^\\\\wsl[\$\.]localhost?\\[^\\]+(.*)$`)
	if matches := wslPattern.FindStringSubmatch(path); len(matches) > 1 {
		return strings.ReplaceAll(matches[1], "\\", "/")
	}

	// Handle Windows drive paths (C:\... -> /mnt/c/...)
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(string(path[0]))
		rest := ""
		if len(path) > 2 {
			rest = path[2:]
		}
		return "/mnt/" + drive + strings.ReplaceAll(rest, "\\", "/")
	}

	return path
}

// getWSLDistro gets the current WSL distro name (when running on Windows)
func getWSLDistro() string {
	// Try to get from environment or wsl command
	if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" {
		return distro
	}

	// Try running wsl to get default distro
	cmd := exec.Command("wsl", "-l", "-q")
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Remove null bytes (UTF-16 artifacts)
			line = strings.ReplaceAll(line, "\x00", "")
			if line != "" {
				return line
			}
		}
	}

	return ""
}

// NormalizePath normalizes a path for the current OS
func NormalizePath(path string) string {
	converted := ConvertPath(path)
	return filepath.Clean(converted)
}
