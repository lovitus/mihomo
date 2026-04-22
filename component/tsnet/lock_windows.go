//go:build windows

package tsnet

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type dirLock struct {
	path string
	file *os.File
	ol   windows.Overlapped
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
	lock := &dirLock{path: path, file: file}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.ol)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	_ = file.Truncate(0)
	_, _ = file.Seek(0, 0)
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	return lock, nil
}

func (l *dirLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.ol)
	return l.file.Close()
}
