// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package metric defines the primitives shared by every telemetry source:
// optional values that distinguish "unavailable" from zero, and provenance
// tags that record where a value came from.
package metric

import (
	"bytes"
	"encoding/json"
)

// Opt is an optional metric value. The zero value is "unavailable".
//
// gputop never turns an unsupported or failed reading into 0: a reading
// that could not be obtained is represented as an invalid Opt, rendered as
// "N/A" in the UI and encoded as null in JSON.
type Opt[T any] struct {
	V  T
	OK bool
}

// Some returns an available value.
func Some[T any](v T) Opt[T] { return Opt[T]{V: v, OK: true} }

// None returns an unavailable value.
func None[T any]() Opt[T] { return Opt[T]{} }

// Get returns the value and whether it is available.
func (o Opt[T]) Get() (T, bool) { return o.V, o.OK }

// Or returns the value if available, otherwise def.
func (o Opt[T]) Or(def T) T {
	if o.OK {
		return o.V
	}
	return def
}

var null = []byte("null")

// MarshalJSON encodes unavailable values as null.
func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if !o.OK {
		return null, nil
	}
	return json.Marshal(o.V)
}

// UnmarshalJSON decodes null as unavailable.
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), null) {
		*o = Opt[T]{}
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*o = Opt[T]{V: v, OK: true}
	return nil
}

// IsZero lets encoders using omitzero skip unavailable values.
func (o Opt[T]) IsZero() bool { return !o.OK }

// Map converts an available value with f.
func Map[T, U any](o Opt[T], f func(T) U) Opt[U] {
	if !o.OK {
		return Opt[U]{}
	}
	return Some(f(o.V))
}

// Float converts any available numeric value to float64.
func Float[T ~int | ~int32 | ~int64 | ~uint | ~uint32 | ~uint64 | ~float32 | ~float64](o Opt[T]) Opt[float64] {
	if !o.OK {
		return Opt[float64]{}
	}
	return Some(float64(o.V))
}
