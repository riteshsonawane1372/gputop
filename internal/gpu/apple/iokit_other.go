// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package apple

import (
	"errors"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
)

type unsupportedBackend struct{}

func newIOKitBackend() backend { return unsupportedBackend{} }

func (unsupportedBackend) Open() (staticInfo, []gpu.Check, error) {
	return staticInfo{}, nil, errors.New("apple GPU monitoring is only available on macOS")
}
func (unsupportedBackend) Read() reading     { return reading{} }
func (unsupportedBackend) Clients() []client { return nil }
func (unsupportedBackend) Close()            {}
