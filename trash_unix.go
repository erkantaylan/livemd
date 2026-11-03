//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// moveToTrash puts path in the user's trash per the freedesktop.org Trash
// specification, so the file manager's Trash shows it and can restore it:
// the file goes to $XDG_DATA_HOME/Trash/files and a .trashinfo beside it in
// Trash/info records where it came from and when.
func moveToTrash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	trash, err := homeTrashDir()
	if err != nil {
		return err
	}
	filesDir := filepath.Join(trash, "files")
	infoDir := filepath.Join(trash, "info")
	for _, d := range []string{filesDir, infoDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("trash: %w", err)
		}
	}

	info := trashInfo(abs, time.Now())
	name, infoPath, err := reserveTrashName(filesDir, infoDir, filepath.Base(abs), info)
	if err != nil {
		return err
	}
	dest := filepath.Join(filesDir, name)

	if err := os.Rename(abs, dest); err != nil {
		// The spec wants a per-volume .Trash-$uid for files on another device.
		// The inbox lives under $HOME, so that only happens when XDG_DATA_HOME
		// points at another disk; a copy is the honest fallback there.
		if !errors.Is(err, syscall.EXDEV) {
			os.Remove(infoPath)
			return fmt.Errorf("trash: %w", err)
		}
		if err := copyThenRemove(abs, dest); err != nil {
			os.Remove(infoPath)
			return fmt.Errorf("trash: %w", err)
		}
	}
	return nil
}

func homeTrashDir() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "Trash"), nil
}

// trashInfo is the .trashinfo body. Path is URL-escaped with its slashes kept,
// and DeletionDate is local time without a zone, both as the spec requires.
func trashInfo(abs string, when time.Time) string {
	escaped := (&url.URL{Path: abs}).EscapedPath()
	return "[Trash Info]\nPath=" + escaped + "\nDeletionDate=" + when.Format("2006-01-02T15:04:05") + "\n"
}

// reserveTrashName claims a name in the trash by creating its .trashinfo with
// O_EXCL — the spec's way of making two concurrent trashings of "notes.md"
// pick different names. A taken name gets " 2", " 3", ... before the extension.
func reserveTrashName(filesDir, infoDir, base, info string) (name, infoPath string, err error) {
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	for i := 1; i < 10000; i++ {
		name = base
		if i > 1 {
			name = stem + " " + strconv.Itoa(i) + ext
		}
		if _, err := os.Lstat(filepath.Join(filesDir, name)); err == nil {
			continue
		}
		infoPath = filepath.Join(infoDir, name+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", "", fmt.Errorf("trash: %w", err)
		}
		_, werr := f.WriteString(info)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			os.Remove(infoPath)
			return "", "", fmt.Errorf("trash: could not write %s", infoPath)
		}
		return name, infoPath, nil
	}
	return "", "", fmt.Errorf("trash: no free name for %s", base)
}

func copyThenRemove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(src)
}
