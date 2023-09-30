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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
)

type policy20 struct {
	model *asserts.Model
	opts  *Options

	warningf func(format string, a ...interface{})
}

var errNotAllowedExceptForDangerous = errors.New("cannot override channels, add devmode snaps, local snaps, or extra snaps with a model of grade higher than dangerous")

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
	return pol.checkAllowedDangerous()
}

func (pol *policy20) checkSnapChannel(ch channel.Channel, whichSnap string) error {
	return pol.checkAllowedDangerous()
}

func (pol *policy20) checkClassicSnap(sn *SeedSnap) error {
	if pol.model.Grade() == asserts.ModelDangerous {
		// implicit classic snaps are accepted
		return nil
	}
	modSnap, ok := sn.SnapRef.(*asserts.ModelSnap)
	if !ok {
		return fmt.Errorf("internal error: extra snap with non-dangerous grade")
	}
	if !modSnap.Classic {
		return fmt.Errorf("cannot use classic snap %q with a model of grade higher than dangerous that does not allow it explicitly (missing classic: true in snap stanza)", modSnap.Name)
	}
	return nil
}

func (pol *policy20) systemSnap() *asserts.ModelSnap {
	return internal.MakeSystemSnap("snapd", "latest/stable", []string{"run", "ephemeral"})
}

func (pol *policy20) modelSnapDefaultChannel() string {
	// We will use latest/stable as default, model that want something else
	// will need to to speficy a default-channel
	return "latest/stable"
}

func (pol *policy20) extraSnapDefaultChannel() string {
	// We will use latest/stable as default for consistency with
	// model snaps, this means not taking into account default-tracks
	// by default
	return "latest/stable"
}

func (pol *policy20) checkBase(info *snap.Info, modes []string, availableByMode map[string]*naming.SnapSet, optionsSnaps []*OptionsSnap) error {
	base := info.Base
	if base == "" {
		if info.Type() != snap.TypeGadget && info.Type() != snap.TypeApp {
			return nil
		}
		base = "core"
	}

	if pol.checkAvailable(naming.Snap(base), modes, availableByMode) {
		return nil
	}

	for _, optSnap := range optionsSnaps {
		if base == optSnap.Name {
			return nil
		} else if base == "" && optSnap.Name == "core" {
			return nil
		}
	}

	whichBase := fmt.Sprintf("its base %q", base)
	return fmt.Errorf("cannot add snap %q without also adding %s explicitly%s", info.SnapName(), whichBase, errorMsgForModesSuffix(modes))
}

func (pol *policy20) checkAvailable(snapRef naming.SnapRef, modes []string, availableByMode map[string]*naming.SnapSet) bool {
	// checks that snapRef is available in all modes
	for _, mode := range modes {
		byMode := availableByMode[mode]
		if !byMode.Contains(snapRef) {
			if mode == "run" || mode == "ephemeral" {
				// no additional fallback for these
				// cases:
				// * run is not ephemeral,
				//   is covered only by run
				// * ephemeral is only covered by ephemeral
				return false
			}
			// all non-run modes (e.g. recover) are
			// considered ephemeral, as a fallback check
			// if the snap is listed under the ephemeral mode label
			ephem := availableByMode["ephemeral"]
			if ephem == nil || !ephem.Contains(snapRef) {
				return false
			}
		}
	}
	return true
}

func (pol *policy20) needsImplicitSnaps(map[string]*naming.SnapSet) (bool, error) {
	// no implicit snaps with Core 20
	// TODO: unless we want to support them for extra snaps
	return false, nil
}

func (pol *policy20) implicitSnaps(map[string]*naming.SnapSet) []*asserts.ModelSnap {
	return nil
}

func (pol *policy20) implicitExtraSnaps(map[string]*naming.SnapSet) []*OptionsSnap {
	return nil
}

func (pol *policy20) recordSnapNameUsage(_ string) {}

func (pol *policy20) isSystemSnapCandidate(sn *SeedSnap) bool {
	if sn.modelSnap != nil {
		return sn.modelSnap.SnapType == "snapd"
	}
	return false
}

func (pol *policy20) ignoreUndeterminedSystemSnap() bool {
	// a system snap should always be known
	return false
}

type tree20 struct {
	grade asserts.ModelGrade
	opts  *Options

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

	if err := os.MkdirAll(filepath.Dir(tr.systemDir), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(tr.systemDir, 0755); err != nil {
		if os.IsExist(err) {
			return &SystemAlreadyExistsError{
				label: tr.opts.Label,
			}
		}
		return err
	}
	return nil
}

func (tr *tree20) ensureSystemSnapsDir() (string, error) {
	snapsDir := filepath.Join(tr.systemDir, "snaps")
	if tr.systemSnapsDirEnsured {
		return snapsDir, nil
	}
	if err := os.MkdirAll(snapsDir, 0755); err != nil {
		return "", err
	}
	tr.systemSnapsDirEnsured = true
	return snapsDir, nil
}

func (tr *tree20) snapPath(sn *SeedSnap) (string, error) {
	var snapsDir string
	if sn.modelSnap != nil {
		snapsDir = tr.snapsDirPath
	} else {
		// extra snap
		var err error
		snapsDir, err = tr.ensureSystemSnapsDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(snapsDir, sn.Info.Filename()), nil
}

func (tr *tree20) localSnapPath(sn *SeedSnap) (string, error) {
	sysSnapsDir, err := tr.ensureSystemSnapsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(sysSnapsDir, fmt.Sprintf("%s_%s.snap", sn.SnapName(), sn.Info.Version)), nil
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
					for _, aRef := range sn.aRefs {
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

func (tr *tree20) writeMeta(snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error {
	var optionsSnaps []*internal.Snap20

	for _, sn := range snapsFromModel {
		channelOverride := ""
		if sn.Channel != sn.modelSnap.DefaultChannel {
			channelOverride = sn.Channel
		}
		if sn.Info.ID() != "" && channelOverride == "" {
			continue
		}
		unasserted := ""
		if sn.Info.ID() == "" {
			unasserted = filepath.Base(sn.Path)
		}

		optionsSnaps = append(optionsSnaps, &internal.Snap20{
			Name: sn.SnapName(),
			// even if unasserted != "" SnapID is useful
			// to cross-ref the model entry
			SnapID:     sn.modelSnap.ID(),
			Unasserted: unasserted,
			Channel:    channelOverride,
		})
	}

	for _, sn := range extraSnaps {
		channel := sn.Channel
		unasserted := ""
		if sn.Info.ID() == "" {
			unasserted = filepath.Base(sn.Path)
			channel = ""
		}

		optionsSnaps = append(optionsSnaps, &internal.Snap20{
			Name:       sn.SnapName(),
			SnapID:     sn.Info.ID(),
			Unasserted: unasserted,
			Channel:    channel,
		})
	}

	if len(optionsSnaps) != 0 {
		if tr.grade != asserts.ModelDangerous {
			return fmt.Errorf("internal error: unexpected non-model snap overrides with grade %s", tr.grade)
		}
		options20 := &internal.Options20{Snaps: optionsSnaps}
		if err := options20.Write(filepath.Join(tr.systemDir, "options.yaml")); err != nil {
			return err
		}
	}

	auxInfos := make(map[string]*internal.AuxInfo20)

	addAuxInfos := func(seedSnaps []*SeedSnap) {
		for _, sn := range seedSnaps {
			if sn.Info.ID() != "" {
				if len(sn.Info.Links()) != 0 || sn.Info.Private {
					auxInfos[sn.Info.ID()] = &internal.AuxInfo20{
						Private: sn.Info.Private,
						Links:   sn.Info.Links(),
						Contact: sn.Info.Contact(),
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

	if _, err := tr.ensureSystemSnapsDir(); err != nil {
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
