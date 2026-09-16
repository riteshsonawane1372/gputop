// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Command gputop is a terminal GPU monitor.
package main

import (
	"os"

	"github.com/riteshsonawane1372/gputop/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
