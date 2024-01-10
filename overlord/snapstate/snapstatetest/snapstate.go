// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snapstatetest

import (
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/snap"
)

func NewSequenceFromSnapSideInfos(snapSideInfo []*snap.SideInfo) sequence.SnapSequence {
	revSis := make([]*sequence.RevisionSideState, len(snapSideInfo))
	for i, si := range snapSideInfo {
		revSis[i] = sequence.NewRevisionSideInfo(si, nil)
	}
	return sequence.SnapSequence{Revisions: revSis}
}

func NewSequenceFromRevisionSideInfos(revsSideInfo []*sequence.RevisionSideState) sequence.SnapSequence {
	return sequence.SnapSequence{Revisions: revsSideInfo}
}
