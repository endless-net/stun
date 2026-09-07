package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"runtime"
)

func currentExecutableDigest() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		// /proc/self/exe follows the running inode, not a mutable current
		// symlink that an Infrastructure activation may switch concurrently.
		path = "/proc/self/exe"
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}
