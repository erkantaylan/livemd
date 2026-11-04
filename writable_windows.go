//go:build windows

package main

import "os"

// fileWritable reports whether this process may write path. Go maps the
// read-only attribute to a missing write bit, but ACLs and another program
// holding the file locked are invisible to Stat, so the only reliable test is
// to open it for writing. Nothing is written, so the file is not touched.
func fileWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
