//go:build !windows

package tsnet

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type dirLock struct {
	path string
	file *os.File
}

func lockStateDir(dir string) (*dirLock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".mihomo-tsnet.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	_ = file.Truncate(0)
	_, _ = file.Seek(0, 0)
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	return &dirLock{path: path, file: file}, nil
}

func (l *dirLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	return l.file.Close()
}
