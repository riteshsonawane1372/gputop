// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package metric

import (
	"encoding/json"
	"testing"
)

func TestOptJSON(t *testing.T) {
	type rec struct {
		A Opt[float64] `json:"a"`
		B Opt[uint64]  `json:"b"`
		C Opt[bool]    `json:"c"`
	}
	b, err := json.Marshal(rec{A: Some(1.5), C: Some(false)})
	if err != nil || string(b) != `{"a":1.5,"b":null,"c":false}` {
		t.Fatalf("%s %v", b, err)
	}
	var r rec
	if err := json.Unmarshal([]byte(`{"a":null,"b":7,"c":true}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.A.OK || !r.B.OK || r.B.V != 7 || !r.C.V {
		t.Fatalf("%+v", r)
	}
	if Some(0.0) == None[float64]() {
		t.Fatal("zero must differ from unavailable")
	}
	if None[int]().Or(3) != 3 || Map(Some(2), func(v int) string { return "x" }).V != "x" || Float(Some(uint64(4))).V != 4 {
		t.Fatal("helpers")
	}
}
