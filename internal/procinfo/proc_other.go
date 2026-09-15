// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package procinfo

func nestedPIDNamespace() bool { return false }

func startTicks(pid int) (uint64, bool) { return 0, false }

func readInfo(pid int, start uint64) Info {
	return Info{PID: pid, Reason: "process metadata is only resolved on Linux"}
}
