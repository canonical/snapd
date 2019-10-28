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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
)

var validSystemLabel = regexp.MustCompile("^[a-zA-Z0-9]+(?:-[a-zA-Z0-9]+)*$")

func validateSystemLabel(label string) error {
	if !validSystemLabel.MatchString(label) {
		return fmt.Errorf("system label contains invalid characters: %s", label)
	}
	return nil
}

type policy20 struct {
	model *asserts.Model
	opts  *Options

	warningf func(format string, a ...interface{})
}

var errNotAllowedExceptForDangerous = errors.New("cannot override channels, add local snaps or extra snaps with a model of grade higher than dangerous")

func (pol *policy20) checkAllowedDangerous() error {
	if pol.model.Grade() != asserts.ModelDangerous {
		return errNotAllowedExceptForDangerous
	}
	return nil
}

func (pol *policy20) allowsDangerousFeatures() error {
	return pol.checkAllowedDangerous()
}

func (pol *policy20) checkDefaultChannel(channel.Channel) error {
	// TODO: consider allowing some channel overrides for >=signed
	// Core 20 models?
	return pol.checkAllowedDangerous()
}

func (pol *policy20) checkSnapChannel(ch channel.Channel, whichSnap string) error {
	// TODO: consider allowing some channel overrides for >=signed
	// Core 20 models?
	return pol.checkAllowedDangerous()
}

func (pol *policy20) systemSnap() *asserts.ModelSnap {
	return makeSystemSnap("snapd")
}

func (pol *policy20) modelSnapDefaultChannel() string {
	// We will use latest/stable as default, model that want something else
	// will need to to speficy a default-channel
	return "latest/stable"
}

func (pol *policy20) extraSnapDefaultChannel() string {
	// We will use latest/stable as default
	// TODO: consider using just "stable" for these?
	return "latest/stable"
}

func (pol *policy20) checkBase(info *snap.Info, availableSnaps *naming.SnapSet) error {
	base := info.Base
	if base == "" {
		if info.GetType() != snap.TypeGadget && info.GetType() != snap.TypeApp {
			return nil
		}
		base = "core"
	}

	if availableSnaps.Contains(naming.Snap(base)) {
		return nil
	}

	whichBase := fmt.Sprintf("its base %q", base)
	if base == "core16" {
		if availableSnaps.Contains(naming.Snap("core")) {
			return nil
		}
		whichBase += ` (or "core")`
	}

	return fmt.Errorf("cannot add snap %q without also adding %s explicitly", info.SnapName(), whichBase)
}

func (pol *policy20) needsImplicitSnaps(*naming.SnapSet) (bool, error) {
	// no implicit snaps with Core 20
	// TODO: unless we want to support them for extra snaps
	return false, nil
}

func (pol *policy20) implicitSnaps(*naming.SnapSet) []*asserts.ModelSnap {
	return nil
}

func (pol *policy20) implicitExtraSnaps(*naming.SnapSet) []*OptionsSnap {
	return nil
}

type tree20 struct {
	opts *Options

	snapsDirPath string
	systemDir    string

	systemSnapsDirEnsured bool
}

func (tr *tree20) mkFixedDirs() error {
	tr.snapsDirPath = filepath.Join(tr.opts.SeedDir, "snaps")
	tr.systemDir = filepath.Join(tr.opts.SeedDir, "systems", tr.opts.Label)

	if err := os.MkdirAll(tr.snapsDirPath, 0755); err != nil {
		return err
	}

	return os.MkdirAll(tr.systemDir, 0755)
}

func (tr *tree20) ensureSystemSnapsDir() error {
	if tr.systemSnapsDirEnsured {
		return nil
	}
	snapsDir := filepath.Join(tr.systemDir, "snaps")
	if err := os.MkdirAll(snapsDir, 0755); err != nil {
		return err
	}
	tr.systemSnapsDirEnsured = true
	return nil
}

func (tr *tree20) snapsDir() string {
	// XXX what about extra snaps?
	return tr.snapsDirPath
}

func (tr *tree20) localSnapPath(*SeedSnap) string {
	panic("XXX dangerous features not implemented yet")
}

func (tr *tree20) writeAssertions(db asserts.RODatabase, modelRefs []*asserts.Ref, snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error {
	assertsDir := filepath.Join(tr.systemDir, "assertions")
	if err := os.MkdirAll(assertsDir, 0755); err != nil {
		return err
	}

	writeByRefs := func(fname string, refsGen func(stop <-chan struct{}) <-chan *asserts.Ref) error {
		f, err := os.OpenFile(filepath.Join(assertsDir, fname), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		defer f.Close()

		stop := make(chan struct{})
		defer close(stop)
		refs := refsGen(stop)

		enc := asserts.NewEncoder(f)
		for {
			aRef := <-refs
			if aRef == nil {
				break
			}
			a, err := aRef.Resolve(db.Find)
			if err != nil {
				return fmt.Errorf("internal error: lost saved assertion")
			}
			if err := enc.Encode(a); err != nil {
				return err
			}
		}
		return nil
	}

	pushRef := func(refs chan<- *asserts.Ref, ref *asserts.Ref, stop <-chan struct{}) bool {
		select {
		case refs <- ref:
			return true
		case <-stop:
			// get unstuck if we error early
			return false
		}
	}

	modelOnly := func(aRef *asserts.Ref) bool { return aRef.Type == asserts.ModelType }
	excludeModel := func(aRef *asserts.Ref) bool { return aRef.Type != asserts.ModelType }

	modelRefsGen := func(include func(*asserts.Ref) bool) func(stop <-chan struct{}) <-chan *asserts.Ref {
		return func(stop <-chan struct{}) <-chan *asserts.Ref {
			refs := make(chan *asserts.Ref)
			go func() {
				for _, aRef := range modelRefs {
					if include(aRef) {
						if !pushRef(refs, aRef, stop) {
							return
						}
					}
				}
				close(refs)
			}()
			return refs
		}
	}

	if err := writeByRefs("../model", modelRefsGen(modelOnly)); err != nil {
		return err
	}

	if err := writeByRefs("model-etc", modelRefsGen(excludeModel)); err != nil {
		return err
	}

	snapsRefGen := func(snaps []*SeedSnap) func(stop <-chan struct{}) <-chan *asserts.Ref {
		return func(stop <-chan struct{}) <-chan *asserts.Ref {
			refs := make(chan *asserts.Ref)
			go func() {
				for _, sn := range snaps {
					for _, aRef := range sn.ARefs {
						if !pushRef(refs, aRef, stop) {
							return
						}
					}
				}
				close(refs)
			}()
			return refs
		}
	}

	if err := writeByRefs("snaps", snapsRefGen(snapsFromModel)); err != nil {
		return err
	}

	if len(extraSnaps) != 0 {
		if err := writeByRefs("extra-snaps", snapsRefGen(extraSnaps)); err != nil {
			return err
		}
	}

	return nil
}

type auxInfo20 struct {
	Private bool   `json:"private,omitempty"`
	Contact string `json:"contact,omitempty"`
}

func (tr *tree20) writeMeta(snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error {
	// TODO|XXX: produce an options.yaml if needed (grade dangerous)

	auxInfos := make(map[string]*auxInfo20)

	addAuxInfos := func(seedSnaps []*SeedSnap) {
		for _, sn := range seedSnaps {
			if sn.Info.ID() != "" {
				if sn.Info.Contact != "" || sn.Info.Private {
					auxInfos[sn.Info.ID()] = &auxInfo20{
						Private: sn.Info.Private,
						Contact: sn.Info.Contact,
					}
				}
			}
		}
	}

	addAuxInfos(snapsFromModel)
	addAuxInfos(extraSnaps)

	if len(auxInfos) == 0 {
		// nothing to do
		return nil
	}

	if err := tr.ensureSystemSnapsDir(); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(tr.systemDir, "snaps", "aux-info.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	if err := enc.Encode(auxInfos); err != nil {
		return err
	}

	return nil
}
