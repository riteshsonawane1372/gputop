// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"

	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/kube"
	"github.com/gputop/gputop/internal/model"
)

// Source supplies snapshots to the UI. The local collector engine and the
// remote agent client both implement it; the UI never talks to providers.
type Source interface {
	Subscribe() (<-chan *model.Snapshot, func())
	Latest() *model.Snapshot
	RefreshNow()
	// History returns nil when history is disabled.
	History() history.Reader
}

// KubeInspector is optionally implemented by sources that can fetch pod
// logs and events on demand (the local engine; not remote agents).
type KubeInspector interface {
	PodLogs(ctx context.Context, pod kube.PodRef, container string, tail int) (lines []string, source string, err error)
	PodEvents(ctx context.Context, pod kube.PodRef) ([]kube.PodEvent, error)
}

// NodeSummary is a remote node's status for the Nodes tab.
type NodeSummary struct {
	Name     string
	Address  string
	OK       bool
	Error    string
	Snapshot *model.Snapshot
}

// NodeLister optionally provides remote node summaries.
type NodeLister interface {
	Nodes() []NodeSummary
}
