// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package procinfo

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockTicks is USER_HZ, which is 100 on every mainstream Linux
// architecture (x86, arm64, ppc64le, s390x).
const clockTicks = 100

var procRoot = "/proc"

func nestedPIDNamespace() bool {
	f, err := os.Open(procRoot + "/self/status")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "NSpid:") {
			return len(strings.Fields(line)) > 2
		}
	}
	return false
}

// startTicks returns field 22 of /proc/<pid>/stat.
func startTicks(pid int) (uint64, bool) {
	b, err := os.ReadFile(procRoot + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// comm may contain spaces/parentheses: parse after the last ')'.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(string(b[i+1:]))
	// fields[0] is field 3 (state); starttime is field 22 -> index 19.
	if len(fields) < 20 {
		return 0, false
	}
	v, err := strconv.ParseUint(fields[19], 10, 64)
	return v, err == nil
}

var (
	bootOnce sync.Once
	bootTime time.Time
)

func boot() time.Time {
	bootOnce.Do(func() {
		b, err := os.ReadFile(procRoot + "/stat")
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "btime ") {
				if v, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 10, 64); err == nil {
					bootTime = time.Unix(v, 0)
				}
			}
		}
	})
	return bootTime
}

func readInfo(pid int, start uint64) Info {
	dir := procRoot + "/" + strconv.Itoa(pid)
	info := Info{PID: pid, Visible: true}
	if b, err := os.ReadFile(dir + "/comm"); err == nil {
		info.Name = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(dir + "/cmdline"); err == nil {
		info.Command = strings.TrimSpace(strings.ReplaceAll(string(bytes.TrimRight(b, "\x00")), "\x00", " "))
	}
	if b, err := os.ReadFile(dir + "/status"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				if f := strings.Fields(line); len(f) > 1 {
					info.User = f[1]
				}
				break
			}
		}
	}
	if b, err := os.ReadFile(dir + "/cgroup"); err == nil {
		info.Cgroup = string(b)
	}
	if bt := boot(); !bt.IsZero() {
		info.StartTime = bt.Add(time.Duration(start) * time.Second / clockTicks)
	}
	if info.Name == "" && info.Command == "" {
		info.Visible = false
		info.Reason = "process exited"
	}
	return info
}
