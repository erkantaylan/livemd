//go:build !windows

package main

import "golang.org/x/sys/unix"

// fileWritable reports whether this process may write path. access(2) gives
// one answer for every way a file can be read-only here: no write bit for us,
// another owner, a read-only mount (EROFS) or the immutable flag (EPERM).
func fileWritable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil
}
