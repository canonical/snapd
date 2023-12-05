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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

func NewSequenceFromSnapSideInfos(snapSideInfo []*snap.SideInfo) snapstate.SnapSequence {
	revSis := make([]*snapstate.RevisionSideState, len(snapSideInfo))
	for i, si := range snapSideInfo {
		revSis[i] = snapstate.NewRevisionSideInfo(si, nil)
	}
	return snapstate.SnapSequence{Revisions: revSis}
}

func NewSequenceFromRevisionSideInfos(revsSideInfo []*snapstate.RevisionSideState) snapstate.SnapSequence {
	return snapstate.SnapSequence{Revisions: revsSideInfo}
}
