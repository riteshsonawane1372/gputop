// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration is a time.Duration that decodes from strings like "30m" or from
// a plain number of seconds.
type Duration time.Duration

// D returns the value as time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: expected a duration like \"30s\"", n.Line)
	}
	s := strings.TrimSpace(n.Value)
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		*d = Duration(time.Duration(secs * float64(time.Second)))
		return nil
	}
	v, err := parseDuration(s)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q (use e.g. 500ms, 5s, 30m, 24h, 7d)", n.Line, s)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return FormatDuration(time.Duration(d)), nil }

// parseDuration extends time.ParseDuration with a "d" (day) unit.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(s)
}

// FormatDuration renders durations compactly ("30m", "1h30m", "500ms").
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	if d%time.Second == 0 && d >= time.Minute {
		return strings.TrimSuffix(strings.TrimSuffix(d.String(), "0s"), "0m")
	}
	return d.String()
}

// StringList decodes either a single string or a list of strings.
type StringList []string

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *StringList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*l = StringList{n.Value}
		return nil
	case yaml.SequenceNode:
		out := make(StringList, 0, len(n.Content))
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: expected a string", c.Line)
			}
			out = append(out, c.Value)
		}
		*l = out
		return nil
	}
	return fmt.Errorf("line %d: expected a string or list of strings", n.Line)
}

// MarshalYAML renders single-element lists as a scalar.
func (l StringList) MarshalYAML() (any, error) {
	if len(l) == 1 {
		return l[0], nil
	}
	return []string(l), nil
}

// AutoBool is "auto", "true" or "false".
type AutoBool string

const (
	Auto  AutoBool = "auto"
	True  AutoBool = "true"
	False AutoBool = "false"
)

// UnmarshalYAML accepts booleans or the string "auto".
func (a *AutoBool) UnmarshalYAML(n *yaml.Node) error {
	switch strings.ToLower(strings.TrimSpace(n.Value)) {
	case "auto", "":
		*a = Auto
	case "true", "yes", "on":
		*a = True
	case "false", "no", "off":
		*a = False
	default:
		return fmt.Errorf("line %d: expected auto, true or false, got %q", n.Line, n.Value)
	}
	return nil
}
