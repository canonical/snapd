// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

// finderFromFetcher exposes an assertion Finder interface out of a Fetcher.
type finderFromFetcher struct {
	f  asserts.Fetcher
	db asserts.RODatabase
}

func (fnd *finderFromFetcher) Find(assertionType *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error) {
	pk, err := asserts.PrimaryKeyFromHeaders(assertionType, headers)
	if err != nil {
		return nil, err
	}
	ref := &asserts.Ref{
		Type:       assertionType,
		PrimaryKey: pk,
	}
	if err := fnd.f.Fetch(ref); err != nil {
		return nil, err
	}
	return fnd.db.Find(assertionType, headers)
}

// DeriveSideInfo tries to construct a SideInfo for the given snap
// using its digest to fetch the relevant snap assertions. It will
// fail with an asserts.NotFoundError if it cannot find them.
func DeriveSideInfo(snapPath string, rf RefAssertsFetcher, db asserts.RODatabase) (*snap.SideInfo, []*asserts.Ref, error) {
	fnd := &finderFromFetcher{f: rf, db: db}
	prev := len(rf.Refs())
	si, err := snapasserts.DeriveSideInfo(snapPath, fnd)
	if err != nil {
		return nil, nil, err
	}
	return si, rf.Refs()[prev:], nil
}
