// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package procinfo

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// macOS has no PID namespaces.
func nestedPIDNamespace() bool { return false }

// startTicks returns the process start time in microseconds since the epoch,
// which identifies a PID incarnation just like /proc starttime on Linux.
func startTicks(pid int) (uint64, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return 0, false
	}
	tv := kp.Proc.P_starttime
	return uint64(tv.Sec)*1e6 + uint64(tv.Usec), true
}

func readInfo(pid int, start uint64) Info {
	info := Info{PID: pid, Visible: true, StartTime: time.UnixMicro(int64(start))}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return Info{PID: pid, Reason: "process exited"}
	}
	info.Name = unix.ByteSliceToString(kp.Proc.P_comm[:])
	info.User = strconv.FormatUint(uint64(kp.Eproc.Ucred.Uid), 10)
	// kern.procargs2 (exec path and argv) is only readable for processes of
	// the same user unless gputop runs as root; p_comm is the fallback.
	if exe, args, ok := procArgs(pid); ok {
		if base := filepath.Base(exe); base != "" && base != "." {
			info.Name = base
		}
		info.Command = strings.Join(args, " ")
	}
	return info
}

// procArgs parses kern.procargs2: argc (int32), the exec path, padding NULs,
// then argc NUL-terminated arguments followed by the environment.
func procArgs(pid int) (exe string, args []string, ok bool) {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(b) < 4 {
		return "", nil, false
	}
	argc := int(binary.LittleEndian.Uint32(b))
	b = b[4:]
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return "", nil, false
	}
	exe = string(b[:i])
	b = bytes.TrimLeft(b[i:], "\x00")
	for len(args) < argc && len(b) > 0 {
		j := bytes.IndexByte(b, 0)
		if j < 0 {
			j = len(b)
		}
		args = append(args, string(b[:j]))
		b = b[min(len(b), j+1):]
	}
	return exe, args, true
}
