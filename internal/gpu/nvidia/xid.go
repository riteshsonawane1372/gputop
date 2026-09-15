// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package nvidia

import "fmt"

// LookupXID returns the catalog entry for an Xid code.
func LookupXID(code uint64) (XIDInfo, bool) {
	info, ok := xidCatalog[code]
	return info, ok
}

// XIDDescription returns a human readable description of an Xid code.
func XIDDescription(code uint64) string {
	if info, ok := xidCatalog[code]; ok {
		return info.Description
	}
	return fmt.Sprintf("Xid %d (not in bundled catalog)", code)
}

// XIDSeverity classifies an Xid using the catalog's immediate action:
// actions requiring a GPU reset, node reboot or an NVIDIA workflow are
// "critical", application restarts are "warning", everything else "info".
// Codes missing from the catalog are treated as "warning".
func XIDSeverity(code uint64) string {
	info, ok := xidCatalog[code]
	if !ok {
		return "warning"
	}
	switch a := info.Action; {
	case a == "RESET_GPU", a == "RESTART_BM", a == "RESTART_VM", a == "CONTACT_SUPPORT",
		a == "CHECK_MECHANICALS", len(a) > 9 && a[:9] == "WORKFLOW_":
		return "critical"
	case a == "RESTART_APP", a == "UPDATE_SWFW", a == "CHECK_UVM", a == "XID_154":
		return "warning"
	}
	return "info"
}
