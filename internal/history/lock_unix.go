// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package history

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var errLocked = errors.New("history directory is in use by another gputop process")

type dirLock struct{ f *os.File }

func lockDir(dir string) (*dirLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return &dirLock{f: f}, nil
}

func (l *dirLock) unlock() {
	if l != nil && l.f != nil {
		_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
		_ = l.f.Close()
	}
}
