// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package history

import "errors"

var errLocked = errors.New("history directory is in use by another gputop process")

type dirLock struct{}

func lockDir(dir string) (*dirLock, error) { return &dirLock{}, nil }

func (l *dirLock) unlock() {}
