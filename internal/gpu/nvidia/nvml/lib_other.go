// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package nvml

// Load reports that NVML is unavailable. NVIDIA stopped shipping macOS
// drivers; Windows support (nvml.dll) is a possible future addition.
func Load(paths []string) (API, error) {
	return nil, ErrPlatformUnsupported
}
