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

	"github.com/snapcore/snapd/asserts"
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

func checkType(sn *SeedSnap, model *asserts.Model) error {
	if sn.modelSnap == nil {
		return nil
	}
	expectedType := snap.TypeApp
	what := ""
	switch sn.modelSnap.SnapType {
	case "snapd":
		expectedType = snap.TypeSnapd
		what = "snapd snap"
	case "core":
		expectedType = snap.TypeOS
		what = "core snap"
	case "base":
		expectedType = snap.TypeBase
		what = fmt.Sprintf("base %q", sn.SnapName())
		if sn.SnapName() == model.Base() {
			what = "boot " + what
		}
	case "kernel":
		expectedType = snap.TypeKernel
		what = fmt.Sprintf("kernel %q", sn.SnapName())
	case "gadget":
		expectedType = snap.TypeGadget
		what = fmt.Sprintf("gadget %q", sn.SnapName())
	case "app":
		expectedType = snap.TypeApp
		what = fmt.Sprintf("snap %q", sn.SnapName())
	case "":
		// ModelSnap for Core 16/18 "required-snaps" have
		// SnapType not set given the model assertion does not
		// have the information
		typ := sn.Info.GetType()
		if typ == snap.TypeKernel || typ == snap.TypeGadget {
			return fmt.Errorf("snap %q has unexpected type: %s", sn.SnapName(), typ)
		}
		return nil
	}
	if sn.Info.GetType() != expectedType {
		return fmt.Errorf("%s has unexpected type: %s", what, sn.Info.GetType())
	}
	return nil
}

type seedSnapsByType []*SeedSnap

func (s seedSnapsByType) Len() int      { return len(s) }
func (s seedSnapsByType) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s seedSnapsByType) Less(i, j int) bool {
	return s[i].Info.GetType().SortsBefore(s[j].Info.GetType())
}
