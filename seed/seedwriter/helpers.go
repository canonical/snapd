// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package seedwriter

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

// A RefAssertsFetcher is a Fetcher that can at any point return
// references to the fetched assertions.
type RefAssertsFetcher interface {
	asserts.Fetcher
	Refs() []*asserts.Ref
	ResetRefs()
}

type refRecFetcher struct {
	asserts.Fetcher
	refs []*asserts.Ref
}

func (rrf *refRecFetcher) Refs() []*asserts.Ref {
	return rrf.refs
}

func (rrf *refRecFetcher) ResetRefs() {
	rrf.refs = nil
}

// A NewFetcherFunc can build a Fetcher saving to an (implicit)
// database and also calling the given additional save function.
type NewFetcherFunc func(save func(asserts.Assertion) error) asserts.Fetcher

// MakeRefAssertsFetcher makes a RefAssertsFetcher using newFetcher which can
// build a base Fetcher with an additional save function.
func MakeRefAssertsFetcher(newFetcher NewFetcherFunc) RefAssertsFetcher {
	var rrf refRecFetcher
	save := func(a asserts.Assertion) error {
		rrf.refs = append(rrf.refs, a.Ref())
		return nil
	}
	rrf.Fetcher = newFetcher(save)
	return &rrf
}

func whichModelSnap(modSnap *asserts.ModelSnap, model *asserts.Model) string {
	switch modSnap.SnapType {
	case "snapd":
		return "snapd snap"
	case "core":
		return "core snap"
	case "base":
		what := fmt.Sprintf("base %q", modSnap.SnapName())
		if modSnap.SnapName() == model.Base() {
			what = "boot " + what
		}
		return what
	case "kernel":
		return fmt.Sprintf("kernel %q", modSnap.SnapName())
	case "gadget":
		return fmt.Sprintf("gadget %q", modSnap.SnapName())
	default:
		return fmt.Sprintf("snap %q", modSnap.SnapName())
	}
}

func checkType(sn *SeedSnap, model *asserts.Model) error {
	if sn.modelSnap == nil {
		return nil
	}
	expectedType := snap.TypeApp
	switch sn.modelSnap.SnapType {
	case "snapd":
		expectedType = snap.TypeSnapd
	case "core":
		expectedType = snap.TypeOS
	case "base":
		expectedType = snap.TypeBase
	case "kernel":
		expectedType = snap.TypeKernel
	case "gadget":
		expectedType = snap.TypeGadget
	case "app":
		expectedType = snap.TypeApp
	case "":
		// ModelSnap for Core 16/18 "required-snaps" have
		// SnapType not set given the model assertion does not
		// have the information
		typ := sn.Info.Type()
		if typ == snap.TypeKernel || typ == snap.TypeGadget {
			return fmt.Errorf("snap %q has unexpected type: %s", sn.SnapName(), typ)
		}
		return nil
	}
	if sn.Info.Type() != expectedType {
		what := whichModelSnap(sn.modelSnap, model)
		return fmt.Errorf("%s has unexpected type: %s", what, sn.Info.Type())
	}
	return nil
}

func errorMsgForModesSuffix(modes []string) string {
	if len(modes) == 1 && modes[0] == "run" {
		return ""
	}
	return fmt.Sprintf(" for all relevant modes (%s)", strings.Join(modes, ", "))
}

type seedSnapsByType []*SeedSnap

func (s seedSnapsByType) Len() int      { return len(s) }
func (s seedSnapsByType) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s seedSnapsByType) Less(i, j int) bool {
	return s[i].Info.Type().SortsBefore(s[j].Info.Type())
}

// DeriveSideInfo tries to construct a SideInfo for the given snap
// using its digest to fetch the relevant snap assertions. It will
// fail with an asserts.NotFoundError if it cannot find them.
// model is used to cross check that the found snap-revision is applicable
// on the device.
func DeriveSideInfo(snapPath string, model *asserts.Model, rf RefAssertsFetcher, db asserts.RODatabase) (*snap.SideInfo, []*asserts.Ref, error) {
	digest, size, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return nil, nil, err
	}
	// XXX assume that the input to the writer is trusted or the whole
	// build is isolated
	snapf, err := snapfile.Open(snapPath)
	if err != nil {
		return nil, nil, err
	}
	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		return nil, nil, err
	}
	prev := len(rf.Refs())
	if err := snapasserts.FetchSnapAssertions(rf, digest, info.Provenance()); err != nil {
		return nil, nil, err
	}
	si, err := snapasserts.DeriveSideInfoFromDigestAndSize(snapPath, digest, size, model, db)
	if err != nil {
		return nil, nil, err
	}
	return si, rf.Refs()[prev:], nil
}
