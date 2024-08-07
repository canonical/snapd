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
func DeriveSideInfo(snapPath string, model *asserts.Model, sf SeedAssertionFetcher, db asserts.RODatabase) (*snap.SideInfo, []*asserts.Ref, error) {
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
	prev := len(sf.Refs())
	if err := snapasserts.FetchSnapAssertions(sf, digest, info.Provenance()); err != nil {
		return nil, nil, err
	}
	si, err := snapasserts.DeriveSideInfoFromDigestAndSize(snapPath, digest, size, model, db)
	if err != nil {
		return nil, nil, err
	}
	return si, sf.Refs()[prev:], nil
}

// DeriveComponentSideInfo tries to construct a ComponentSideInfo for the given
// component using its digest to fetch the relevant assertions. It will fail
// with an asserts.NotFoundError if it cannot find them. model is used to cross
// check that the found snap-resource-revision is applicable on the device.
func DeriveComponentSideInfo(compPath string, compInfo *snap.ComponentInfo, info *snap.Info, sf SeedAssertionFetcher, db asserts.RODatabase) (*snap.ComponentSideInfo, []*asserts.Ref, error) {
	// We assume provenance cross-checks for the snap-revision already
	// happened, and here we just check that provenance is consistent
	// between snap and component.
	if info.Provenance() != compInfo.Provenance() {
		return nil, nil, fmt.Errorf("component provenance %s does not match the snap provenance %s", compInfo.Provenance(), info.Provenance())
	}

	digest, size, err := asserts.SnapFileSHA3_384(compPath)
	if err != nil {
		return nil, nil, err
	}

	prev := len(sf.Refs())

	if err := snapasserts.FetchResourceRevisionAssertion(sf, &info.SideInfo,
		compInfo.Component.ComponentName, digest, compInfo.Provenance()); err != nil {
		return nil, nil, err
	}

	csi, err := snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		compInfo.Component.ComponentName, compInfo.Component.SnapName,
		info.ID(), compPath, digest, size, db)
	if err != nil {
		return nil, nil, err
	}

	if err := snapasserts.FetchResourcePairAssertion(sf, &info.SideInfo,
		compInfo.Component.ComponentName, csi.Revision, compInfo.Provenance()); err != nil {
		return nil, nil, err
	}

	return csi, sf.Refs()[prev:], nil
}
