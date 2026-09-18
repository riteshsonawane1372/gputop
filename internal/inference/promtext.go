// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// series is one parsed sample of the Prometheus text exposition format.
type series struct {
	name   string
	labels map[string]string
	value  float64
}

// parseText parses the Prometheus text exposition format (0.0.4) and the
// sample lines of OpenMetrics. Comments, timestamps and exemplars are
// ignored; malformed lines are skipped rather than failing the scrape.
func parseText(b []byte) []series {
	var out []series
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if s, err := parseLine(line); err == nil {
			out = append(out, s)
		}
	}
	return out
}

func parseLine(line string) (series, error) {
	var s series
	i := strings.IndexAny(line, "{ \t")
	if i <= 0 {
		return s, fmt.Errorf("no value")
	}
	s.name = line[:i]
	rest := line[i:]
	if rest[0] == '{' {
		labels, n, err := parseLabels(rest)
		if err != nil {
			return s, err
		}
		s.labels = labels
		rest = rest[n:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return s, fmt.Errorf("no value")
	}
	v, err := parseFloat(fields[0])
	if err != nil {
		return s, err
	}
	s.value = v
	return s, nil
}

// parseLabels parses `{a="x",b="y"}` and returns the bytes consumed.
func parseLabels(s string) (map[string]string, int, error) {
	labels := map[string]string{}
	i := 1
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			return nil, 0, fmt.Errorf("unterminated labels")
		}
		if s[i] == '}' {
			return labels, i + 1, nil
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			return nil, 0, fmt.Errorf("label without value")
		}
		key := strings.TrimSpace(s[i : i+eq])
		i += eq + 1
		if i >= len(s) || s[i] != '"' {
			return nil, 0, fmt.Errorf("unquoted label value")
		}
		i++
		var b strings.Builder
		for ; i < len(s) && s[i] != '"'; i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte(s[i])
				}
				continue
			}
			b.WriteByte(s[i])
		}
		if i >= len(s) {
			return nil, 0, fmt.Errorf("unterminated label value")
		}
		i++ // closing quote
		labels[key] = b.String()
	}
}

func parseFloat(s string) (float64, error) {
	switch s {
	case "+Inf", "Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}
	return strconv.ParseFloat(s, 64)
}
