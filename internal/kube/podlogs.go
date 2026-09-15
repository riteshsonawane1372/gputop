// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PodRef identifies a pod.
type PodRef struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// ScanPodLogs maps pod UIDs to names using the kubelet's pod log directory
// layout: <dir>/<namespace>_<pod-name>_<pod-uid>/. Namespaces and pod names
// cannot contain '_', so the split is unambiguous.
func ScanPodLogs(dir string) (map[string]PodRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PodRef, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		parts := strings.Split(e.Name(), "_")
		if len(parts) != 3 || len(parts[2]) != 36 {
			continue
		}
		out[parts[2]] = PodRef{Namespace: parts[0], Name: parts[1], UID: parts[2]}
	}
	return out, nil
}

// ReadContainerLog returns the last tail lines of a container's current log
// file from the kubelet's pod log directory
// (<dir>/<namespace>_<pod>_<uid>/<container>/<restart>.log). CRI log framing
// ("<time> <stream> <P|F> <text>") is removed and partial lines are joined.
func ReadContainerLog(dir string, pod PodRef, container string, tail int) ([]string, error) {
	cdir := filepath.Join(dir, pod.Namespace+"_"+pod.Name+"_"+pod.UID, container)
	entries, err := os.ReadDir(cdir)
	if err != nil {
		return nil, err
	}
	latest, latestN := "", -1
	for _, e := range entries {
		name := e.Name()
		n, err := strconv.Atoi(strings.TrimSuffix(name, ".log"))
		if err != nil || !strings.HasSuffix(name, ".log") {
			continue
		}
		if n > latestN {
			latest, latestN = name, n
		}
	}
	if latest == "" {
		return nil, fmt.Errorf("no log files in %s", cdir)
	}
	f, err := os.Open(filepath.Join(cdir, latest))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Read at most the last 4 MiB: plenty for a few thousand lines.
	const window = 4 << 20
	if st, err := f.Stat(); err == nil && st.Size() > window {
		if _, err := f.Seek(st.Size()-window, io.SeekStart); err != nil {
			return nil, err
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var out []string
	partial := ""
	for _, raw := range splitLines(string(b)) {
		text, full := parseCRILine(raw)
		if !full {
			partial += text
			continue
		}
		out = append(out, partial+text)
		partial = ""
	}
	if partial != "" {
		out = append(out, partial)
	}
	if tail > 0 && len(out) > tail {
		out = out[len(out)-tail:]
	}
	return out, nil
}

// parseCRILine strips CRI framing; lines in another format pass through.
func parseCRILine(line string) (text string, full bool) {
	parts := strings.SplitN(line, " ", 4)
	if len(parts) >= 3 && (parts[1] == "stdout" || parts[1] == "stderr") && (parts[2] == "F" || parts[2] == "P") {
		if _, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
			if len(parts) == 3 {
				return "", parts[2] == "F"
			}
			return parts[3], parts[2] == "F"
		}
	}
	return line, true
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
