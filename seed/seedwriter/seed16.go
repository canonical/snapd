// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

type policy16 struct {
	model *asserts.Model
	opts  *Options

	warningf func(format string, a ...interface{})

	needsCore   []string
	needsCore16 []string

	snapdUsedOnClassic bool
}

func (pol *policy16) allowsDangerousFeatures() error {
	// Core 16/18 have allowed dangerous features without constraints
	return nil
}

func (pol *policy16) checkDefaultChannel(channel.Channel) error {
	// Core 16 has no constraints on the default channel
	return nil
}

func (pol *policy16) checkSnapChannel(_ channel.Channel, whichSnap string) error {
	// Core 16 has no constraints on snap channel overrides
	return nil
}

func (pol *policy16) checkClassicSnap(_ *SeedSnap) error {
	// Core 16/18 have no constraints on classic snaps
	return nil
}

func makeSystemSnap(snapName string) *asserts.ModelSnap {
	return internal.MakeSystemSnap(snapName, "", []string{"run"})
}

func (pol *policy16) systemSnap() *asserts.ModelSnap {
	if pol.model.Classic() {
		// no predefined system snap, infer later
		return nil
	}
	snapName := "core"
	if pol.model.Base() != "" {
		snapName = "snapd"
	}
	return makeSystemSnap(snapName)
}

func (pol *policy16) modelSnapDefaultChannel() string {
	// We will use latest or the current default track at image build time
	return "stable"
}

func (pol *policy16) extraSnapDefaultChannel() string {
	// We will use latest or the current default track at image build time
	return "stable"
}

func (pol *policy16) checkBase(info *snap.Info, modes []string, availableByMode map[string]*naming.SnapSet) error {
	availableSnaps := availableByMode["run"]
	// snap needs no base (or it simply needs core which is never listed explicitly): nothing to do
	if info.Base == "" {
		if info.Type() == snap.TypeGadget || info.Type() == snap.TypeApp {
			// remember to make sure we have core installed
			pol.needsCore = append(pol.needsCore, info.SnapName())
		}
		return nil
	}

	if availableSnaps.Contains(naming.Snap(info.Base)) {
		return nil
	}

	if info.Base == "core16" {
		// check at the end
		pol.needsCore16 = append(pol.needsCore16, info.SnapName())
		return nil
	}

	return fmt.Errorf("cannot add snap %q without also adding its base %q explicitly", info.SnapName(), info.Base)
}

func (pol *policy16) needsImplicitSnaps(availableByMode map[string]*naming.SnapSet) (bool, error) {
	availableSnaps := availableByMode["run"]
	// do we need to add implicitly either snapd (or core)
	hasCore := availableSnaps.Contains(naming.Snap("core"))
	if len(pol.needsCore) != 0 && !hasCore {
		if pol.model.Base() != "" {
			pol.warningf("model has base %q but some snaps (%s) require \"core\" as base as well, for compatibility it was added implicitly, adding \"core\" explicitly is recommended", pol.model.Base(), strutil.Quoted(pol.needsCore))
		}
		return true, nil
	}

	if len(pol.needsCore16) != 0 && !hasCore {
		return false, fmt.Errorf(`cannot use %s requiring base "core16" without adding "core16" (or "core") explicitly`, strutil.Quoted(pol.needsCore16))
	}

	if pol.model.Classic() && !availableSnaps.Empty() {
		return true, nil
	}

	return false, nil
}

func (pol *policy16) implicitSnaps(availableByMode map[string]*naming.SnapSet) []*asserts.ModelSnap {
	availableSnaps := availableByMode["run"]
	if len(pol.needsCore) != 0 && !availableSnaps.Contains(naming.Snap("core")) {
		return []*asserts.ModelSnap{makeSystemSnap("core")}
	}
	if pol.model.Classic() && !availableSnaps.Empty() && !availableSnaps.Contains(naming.Snap("snapd")) {
		return []*asserts.ModelSnap{makeSystemSnap("snapd")}
	}
	return nil
}

func (pol *policy16) implicitExtraSnaps(availableByMode map[string]*naming.SnapSet) []*OptionsSnap {
	availableSnaps := availableByMode["run"]
	if len(pol.needsCore) != 0 && !availableSnaps.Contains(naming.Snap("core")) {
		return []*OptionsSnap{{Name: "core"}}
	}
	return nil
}

func (pol *policy16) recordSnapNameUsage(snapName string) {
	if !pol.model.Classic() {
		// nothing to record
		return
	}
	pol.snapdUsedOnClassic = snapName == "snapd"
}

func (pol *policy16) isSystemSnapCandidate(sn *SeedSnap) bool {
	if sn.modelSnap != nil {
		sysSnap := pol.systemSnap()
		if sysSnap != nil && sn.modelSnap.SnapType == sysSnap.SnapType {
			return true
		}
		if sn.modelSnap.SnapType == "snapd" {
			return true
		}
	}
	if pol.model.Classic() {
		if pol.snapdUsedOnClassic {
			return sn.SnapName() == "snapd"
		} else {
			return sn.SnapName() == "core"
		}
	}
	return false
}

func (pol *policy16) ignoreUndeterminedSystemSnap() bool {
	// there are some corner cases where we possibly supported just using
	// the deb for classic
	return pol.model.Classic()
}

type tree16 struct {
	opts *Options

	snapsDirPath string
}

func (tr *tree16) mkFixedDirs() error {
	tr.snapsDirPath = filepath.Join(tr.opts.SeedDir, "snaps")
	return os.MkdirAll(tr.snapsDirPath, 0755)
}

func (tr *tree16) snapPath(sn *SeedSnap) (string, error) {
	return filepath.Join(tr.snapsDirPath, sn.Info.Filename()), nil
}

func (tr *tree16) localSnapPath(sn *SeedSnap) (string, error) {
	return filepath.Join(tr.snapsDirPath, sn.Info.Filename()), nil
}

func (tr *tree16) writeAssertions(db asserts.RODatabase, modelRefs []*asserts.Ref, snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error {
	seedAssertsDir := filepath.Join(tr.opts.SeedDir, "assertions")
	if err := os.MkdirAll(seedAssertsDir, 0755); err != nil {
		return err
	}

	writeByRefs := func(aRefs []*asserts.Ref) error {
		for _, aRef := range aRefs {
			var afn string
			// the names don't matter in practice as long as they don't conflict
			if aRef.Type == asserts.ModelType {
				afn = "model"
			} else {
				afn = fmt.Sprintf("%s.%s", strings.Join(asserts.ReducePrimaryKey(aRef.Type, aRef.PrimaryKey), ","), aRef.Type.Name)
			}
			a, err := aRef.Resolve(db.Find)
			if err != nil {
				return fmt.Errorf("internal error: lost saved assertion")
			}
			if err = os.WriteFile(filepath.Join(seedAssertsDir, afn), asserts.Encode(a), 0644); err != nil {
				return err
			}
		}
		return nil
	}

	if err := writeByRefs(modelRefs); err != nil {
		return err
	}

	for _, sn := range snapsFromModel {
		if err := writeByRefs(sn.aRefs); err != nil {
			return err
		}
	}

	for _, sn := range extraSnaps {
		if err := writeByRefs(sn.aRefs); err != nil {
			return err
		}
	}

	return nil
}

func (tr *tree16) writeMeta(snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error {
	var seedYaml internal.Seed16

	seedSnaps := make(seedSnapsByType, len(snapsFromModel)+len(extraSnaps))
	copy(seedSnaps, snapsFromModel)
	copy(seedSnaps[len(snapsFromModel):], extraSnaps)

	sort.Stable(seedSnaps)

	seedYaml.Snaps = make([]*internal.Snap16, len(seedSnaps))
	for i, sn := range seedSnaps {
		info := sn.Info
		channel := sn.Channel
		unasserted := info.SnapID == ""
		if unasserted {
			// Core 16/18 don't set a channel in the seed
			// for unasserted snaps
			channel = ""
		}
		seedYaml.Snaps[i] = &internal.Snap16{
			Name:    info.SnapName(),
			SnapID:  info.SnapID, // cross-ref
			Channel: channel,
			File:    filepath.Base(sn.Path),
			DevMode: info.NeedsDevMode(),
			Classic: info.NeedsClassic(),
			Contact: info.Contact(),
			// no assertions for this snap were put in the seed
			Unasserted: unasserted,
		}
	}

	seedFn := filepath.Join(tr.opts.SeedDir, "seed.yaml")
	if err := seedYaml.Write(seedFn); err != nil {
		return fmt.Errorf("cannot write seed.yaml: %v", err)
	}

	return nil
}
