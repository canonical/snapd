// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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

// Package sequence contains types representing a sequence of snap
// revisions (with components) that describe current and past states
// of the snap in the system.
package sequence

import (
	"encoding/json"
	"errors"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

// ComponentState contains information about an installed component.
type ComponentState struct {
	SideInfo *snap.ComponentSideInfo `json:"side-info"`
	CompType snap.ComponentType      `json:"type"`
}

// NewComponentState creates a ComponentState from components side information and type.
func NewComponentState(si *snap.ComponentSideInfo, tp snap.ComponentType) *ComponentState {
	return &ComponentState{SideInfo: si, CompType: tp}
}

// RevisionSideState contains the side information for a snap and related components
// installed in the system.
type RevisionSideState struct {
	Snap       *snap.SideInfo
	Components []*ComponentState
}

// revisionSideInfoMarshal is an ancillary structure used exclusively to
// help marshaling of RevisionSideInfo.
type revisionSideInfoMarshal struct {
	// SideInfo is included for compatibility with older snapd state files
	*snap.SideInfo
	Components []*ComponentState `json:"components,omitempty"`
}

// MarshalJSON implements the json.Marshaler interface
func (bsi RevisionSideState) MarshalJSON() ([]byte, error) {
	return json.Marshal(&revisionSideInfoMarshal{bsi.Snap, bsi.Components})
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (bsi *RevisionSideState) UnmarshalJSON(in []byte) error {
	var aux revisionSideInfoMarshal
	if err := json.Unmarshal(in, &aux); err != nil {
		return err
	}
	bsi.Snap = aux.SideInfo
	bsi.Components = aux.Components
	return nil
}

// FindComponent returns the ComponentState if cref is found in the sequence point.
func (rss *RevisionSideState) FindComponent(cref naming.ComponentRef) *ComponentState {
	for _, csi := range rss.Components {
		if csi.SideInfo.Component == cref {
			return csi
		}
	}
	return nil
}

// NewRevisionSideState creates a RevisionSideInfo from snap and
// related components side information.
func NewRevisionSideState(snapSideInfo *snap.SideInfo, compSideInfo []*ComponentState) *RevisionSideState {
	return &RevisionSideState{Snap: snapSideInfo, Components: compSideInfo}
}

// SnapSequence is a container for a slice containing revisions of
// snaps plus related components.
// TODO add methods to access Revisions (length, copy, append) and
// use them in handlers.go and snapstate.go.
type SnapSequence struct {
	// Revisions contains information for a snap revision and
	// components SideInfo.
	Revisions []*RevisionSideState
}

// MarshalJSON implements the json.Marshaler interface. We override the default
// so serialization of the SnapState.Sequence field is compatible to what was
// produced when it was defined as a []*snap.SideInfo. This is also the reason
// to have SnapSequence.UnmarshalJSON and MarshalJSON/UnmarshalJSON for
// RevisionSideState.
func (snapSeq SnapSequence) MarshalJSON() ([]byte, error) {
	return json.Marshal(snapSeq.Revisions)
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (snapSeq *SnapSequence) UnmarshalJSON(in []byte) error {
	aux := []*RevisionSideState{}
	if err := json.Unmarshal(in, &aux); err != nil {
		return err
	}
	snapSeq.Revisions = aux
	return nil
}

// SideInfos returns a slice with all the SideInfos for the snap sequence.
func (snapSeq SnapSequence) SideInfos() []*snap.SideInfo {
	sis := make([]*snap.SideInfo, len(snapSeq.Revisions))
	for i, rev := range snapSeq.Revisions {
		sis[i] = rev.Snap
	}
	return sis
}

// LastIndex returns the last index of the given revision in snapSeq,
// or -1 if the revision was not found.
func (snapSeq *SnapSequence) LastIndex(revision snap.Revision) int {
	for i := len(snapSeq.Revisions) - 1; i >= 0; i-- {
		if snapSeq.Revisions[i].Snap.Revision == revision {
			return i
		}
	}
	return -1
}

var ErrSnapRevNotInSequence = errors.New("snap is not in the sequence")

// AddComponentForRevision adds a component to the last instance of snapRev in
// the sequence.
func (snapSeq *SnapSequence) AddComponentForRevision(snapRev snap.Revision, cs *ComponentState) error {
	snapIdx := snapSeq.LastIndex(snapRev)
	if snapIdx == -1 {
		return ErrSnapRevNotInSequence
	}
	revSt := snapSeq.Revisions[snapIdx]

	if currentCompSt := revSt.FindComponent(cs.SideInfo.Component); currentCompSt != nil {
		// Component already present, replace revision
		*currentCompSt = *cs
		return nil
	}

	// Append new component to components of the current snap
	revSt.Components = append(revSt.Components, cs)
	return nil
}

// RemoveComponentForRevision removes the cref component for the last instance
// of snapRev in the sequence and returns a pointer to it, which might be nil
// if not found.
func (snapSeq *SnapSequence) RemoveComponentForRevision(snapRev snap.Revision, cref naming.ComponentRef) (unlinkedComp *ComponentState) {
	snapIdx := snapSeq.LastIndex(snapRev)
	if snapIdx == -1 {
		return nil
	}

	revSt := snapSeq.Revisions[snapIdx]
	var leftComp []*ComponentState
	for _, csi := range revSt.Components {
		if csi.SideInfo.Component == cref {
			unlinkedComp = csi
			continue
		}
		leftComp = append(leftComp, csi)
	}
	revSt.Components = leftComp
	// might be nil
	return unlinkedComp
}

// ComponentStateForRev returns cref's component side info for the revision
// (sequence point) indicated by revIdx if there is one.
func (snapSeq *SnapSequence) ComponentStateForRev(revIdx int, cref naming.ComponentRef) *ComponentState {
	for _, comp := range snapSeq.Revisions[revIdx].Components {
		if comp.SideInfo.Component == cref {
			return comp
		}
	}
	// component not found
	return nil
}

// IsComponentRevPresent tells us if a given component revision is
// present in the system for this snap.
func (snapSeq *SnapSequence) IsComponentRevPresent(compSi *snap.ComponentSideInfo) bool {
	for _, rev := range snapSeq.Revisions {
		for _, cs := range rev.Components {
			if cs.SideInfo.Equal(compSi) {
				return true
			}
		}
	}
	return false
}

func (snapSeq *SnapSequence) ComponentsForRevision(rev snap.Revision) []*ComponentState {
	for _, rss := range snapSeq.Revisions {
		if rss.Snap.Revision == rev {
			return rss.Components
		}
	}
	return nil
}

func (snapSeq *SnapSequence) ComponentsWithTypeForRev(rev snap.Revision, compType snap.ComponentType) []*snap.ComponentSideInfo {
	comps := snapSeq.ComponentsForRevision(rev)
	kmodComps := make([]*snap.ComponentSideInfo, 0, len(comps))
	for _, comp := range comps {
		if comp.CompType != snap.KernelModulesComponent {
			continue
		}
		kmodComps = append(kmodComps, comp.SideInfo)
	}
	if len(kmodComps) == 0 {
		return nil
	}
	return kmodComps
}

// IsComponentRevInRefSeqPtInAnyOtherSeqPt tells us if the component cref in
// the sequence point defined by refIdx is used in another sequence point too.
func (snapSeq *SnapSequence) IsComponentRevInRefSeqPtInAnyOtherSeqPt(cref naming.ComponentRef, refIdx int) bool {
	// Find component in reference sequence point
	refSeqPt := snapSeq.Revisions[refIdx]
	refSeqPtComp := refSeqPt.FindComponent(cref)
	if refSeqPtComp == nil {
		return false
	}

	// Find if the reference component revision is used elsewhere
	for idx, seqPt := range snapSeq.Revisions {
		if idx == refIdx {
			continue
		}
		compInSeqPt := seqPt.FindComponent(cref)
		if compInSeqPt == nil {
			continue
		}
		if compInSeqPt.SideInfo.Revision == refSeqPtComp.SideInfo.Revision {
			return true
		}
	}

	return false
}

// MinimumLocalRevision returns the the smallest local revision for the
// sequence. Local revisions start at -1 and are counted down. 0 will be
// returned if no local revision for the snap is found.
func (snapSeq *SnapSequence) MinimumLocalRevision() snap.Revision {
	var local snap.Revision
	for _, rev := range snapSeq.Revisions {
		if rev.Snap.Revision.N < local.N {
			local = rev.Snap.Revision
		}
	}
	return local
}

// MinimumLocalComponentRevision returns the smallest local revision for the
// compName component in the sequence. Local revisions start at -1 and are
// counted down. 0 will be returned if no local revision for the component is
// found.
func (snapSeq *SnapSequence) MinimumLocalComponentRevision(compName string) snap.Revision {
	var local snap.Revision
	for _, revSt := range snapSeq.Revisions {
		for _, compSt := range revSt.Components {
			if compSt.SideInfo.Component.ComponentName != compName {
				continue
			}
			if compSt.SideInfo.Revision.N >= local.N {
				continue
			}
			local = compSt.SideInfo.Revision
		}
	}
	return local
}

// HasComponents returns true if the revision at the given index has
// any components installed with it.
func (snapSeq *SnapSequence) HasComponents(revIdx int) bool {
	return len(snapSeq.Revisions[revIdx].Components) > 0
}
