// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package index

import managedgit "github.com/openwaldo/waldo/internal/git"

type Identity struct {
	Remote string `json:"remote,omitempty"`
	Commit string `json:"commit,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

// Identify records the Git identity of an index checkout. A valid index is
// still readable outside Git, in which case the empty identity honestly says
// that no revision pin is available.
func Identify(root string) Identity {
	repository, err := managedgit.Inspect(root)
	if err != nil {
		return Identity{}
	}
	return Identity{
		Remote: repository.Remote,
		Commit: repository.Commit,
		Dirty:  repository.Dirty,
	}
}
