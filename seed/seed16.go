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

package seed

/* ATTN this should *not* use:

* dirs package: it is passed an explicit directory to work on

* release.OnClassic: it assumes classic based on the model classic
  option; consistency between system and model can/must be enforced
  elsewhere

*/

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/timings"
)

type seed16 struct {
	seedDir string

	db asserts.RODatabase

	model *asserts.Model

	yamlSnaps []*internal.Snap16

	essCache map[string]*Snap

	snaps             []*Snap
	essentialSnapsNum int

	usesSnapdSnap bool
}

func (s *seed16) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	if db == nil {
		// a db was not provided, create an internal temporary one

		db, commitTo = mylog.Check3(newMemAssertionsDB(nil))
	}

	assertSeedDir := filepath.Join(s.seedDir, "assertions")
	// collect assertions and find model assertion
	var modelRef *asserts.Ref
	checkForModel := func(ref *asserts.Ref) error {
		if ref.Type == asserts.ModelType {
			if modelRef != nil && modelRef.Unique() != ref.Unique() {
				return fmt.Errorf("cannot have multiple model assertions in seed")
			}
			modelRef = ref
		}
		return nil
	}

	batch := mylog.Check2(loadAssertions(assertSeedDir, checkForModel))

	// verify we have one model assertion
	if modelRef == nil {
		return fmt.Errorf("seed must have a model assertion")
	}
	mylog.Check(commitTo(batch))

	a := mylog.Check2(modelRef.Resolve(db.Find))

	// remember db for later use
	s.db = db
	s.model = a.(*asserts.Model)

	return nil
}

func (s *seed16) Model() *asserts.Model {
	if s.model == nil {
		panic("internal error: model assertion unset (LoadAssertions not called)")
	}
	return s.model
}

func (s *seed16) Brand() (*asserts.Account, error) {
	return findBrand(s, s.db)
}

func (s *seed16) SetParallelism(int) {
	// ignored
}

func (s *seed16) addSnap(sn *internal.Snap16, essType snap.Type, pinnedTrack string, handler SnapHandler, cache map[string]*Snap, tm timings.Measurer) (*Snap, error) {
	path := filepath.Join(s.seedDir, "snaps", sn.File)

	_, defaultHandler := handler.(defaultSnapHandler)

	seedSnap := cache[path]
	// not cached, or ignore the cache if a non-default handler
	// was passed, otherwise it would not be called which could be
	// unexpected
	if seedSnap == nil || !defaultHandler {
		snapChannel := sn.Channel
		if pinnedTrack != "" {
			snapChannel = mylog.Check2(channel.ResolvePinned(pinnedTrack, snapChannel))

			// fallback to using the pinned track directly
		}
		seedSnap = &Snap{
			Channel: snapChannel,
			Classic: sn.Classic,
			DevMode: sn.DevMode,
		}

		var sideInfo snap.SideInfo
		var newPath string
		if sn.Unasserted {

			newPath = mylog.Check2(handler.HandleUnassertedSnap(sn.Name, path, tm))

			sideInfo.RealName = sn.Name
		} else {
			var si *snap.SideInfo

			deriveRev := func(snapSHA3_384 string, snapSize uint64) (snap.Revision, error) {
				if si == nil {
					si = mylog.Check2(snapasserts.DeriveSideInfoFromDigestAndSize(path, snapSHA3_384, snapSize, s.model, s.db))
				}
				return si.Revision, nil
			}
			timings.Run(tm, "derive-side-info", fmt.Sprintf("hash and derive side info for snap %q", sn.Name), func(nested timings.Measurer) {
				var snapSHA3_384 string
				var snapSize uint64
				newPath, snapSHA3_384, snapSize = mylog.Check4(handler.HandleAndDigestAssertedSnap(sn.Name, path, essType, nil, deriveRev, tm))

				// sets si too
				_ = mylog.Check2(deriveRev(snapSHA3_384, snapSize))
			})
			if errors.Is(err, &asserts.NotFoundError{}) {
				return nil, fmt.Errorf("cannot find signatures with metadata for snap %q (%q)", sn.Name, path)
			}

			sideInfo = *si
			sideInfo.Private = sn.Private
			sideInfo.LegacyEditedContact = sn.Contact
		}
		origPath := path
		if newPath != "" {
			path = newPath
		}
		seedSnap.Path = path

		seedSnap.SideInfo = &sideInfo
		if cache != nil {
			cache[origPath] = seedSnap
		}
	}

	s.snaps = append(s.snaps, seedSnap)

	return seedSnap, nil
}

type essentialSnapMissingError struct {
	SnapName string
}

func (e *essentialSnapMissingError) Error() string {
	return fmt.Sprintf("essential snap %q required by the model is missing in the seed", e.SnapName)
}

func (s *seed16) loadYaml() error {
	if s.yamlSnaps != nil {
		return nil
	}

	seedYamlFile := filepath.Join(s.seedDir, "seed.yaml")
	if !osutil.FileExists(seedYamlFile) {
		return ErrNoMeta
	}

	seedYaml := mylog.Check2(internal.ReadSeedYaml(seedYamlFile))

	s.yamlSnaps = seedYaml.Snaps

	return nil
}

func (s *seed16) resetSnaps() {
	// setup essential snaps cache
	if s.essCache == nil {
		// 4 = snapd+base+kernel+gadget
		s.essCache = make(map[string]*Snap, 4)
	}

	s.snaps = nil
	s.essentialSnapsNum = 0
}

func (s *seed16) loadEssentialMeta(essentialTypes []snap.Type, required *naming.SnapSet, handler SnapHandler, added map[string]bool, tm timings.Measurer) error {
	model := s.Model()

	seeding := make(map[string]*internal.Snap16, len(s.yamlSnaps))
	for _, sn := range s.yamlSnaps {
		seeding[sn.Name] = sn
	}

	classic := model.Classic()
	_, usesSnapdSnap := seeding["snapd"]
	usesSnapdSnap = usesSnapdSnap || required.Contains(naming.Snap("snapd"))
	s.usesSnapdSnap = usesSnapdSnap

	baseSnap := "core"
	classicWithSnapd := false
	if model.Base() != "" {
		baseSnap = model.Base()
	}
	if classic && s.usesSnapdSnap {
		classicWithSnapd = true
		// there is no system-wide base as such
		// if there is a gadget we will install its base first though
		baseSnap = ""
	}

	// add the essential snaps
	addEssential := func(snapName string, pinnedTrack string, essType snap.Type) (*Snap, error) {
		// be idempotent
		if added[snapName] {
			return nil, nil
		}

		// filter if required
		if essentialTypes != nil {
			skip := true
			for _, t := range essentialTypes {
				if t == essType {
					skip = false
					break
				}
			}
			if skip {
				return nil, nil
			}
		}

		yamlSnap := seeding[snapName]
		if yamlSnap == nil {
			return nil, &essentialSnapMissingError{SnapName: snapName}
		}

		seedSnap := mylog.Check2(s.addSnap(yamlSnap, essType, pinnedTrack, handler, s.essCache, tm))

		if essType == snap.TypeBase && snapName == "core" {
			essType = snap.TypeOS
		}

		seedSnap.EssentialType = essType
		seedSnap.Essential = true
		seedSnap.Required = true
		added[snapName] = true

		return seedSnap, nil
	}

	// if there are snaps to seed, core/base needs to be seeded too
	if len(s.yamlSnaps) != 0 {
		// ensure "snapd" snap is installed first
		if model.Base() != "" || classicWithSnapd {
			mylog.Check2(addEssential("snapd", "", snap.TypeSnapd))
		}
		if !classicWithSnapd {
			mylog.Check2(addEssential(baseSnap, "", snap.TypeBase))
		}
	}

	if kernelName := model.Kernel(); kernelName != "" {
		mylog.Check2(addEssential(kernelName, model.KernelTrack(), snap.TypeKernel))
	}

	if gadgetName := model.Gadget(); gadgetName != "" {
		gadget := mylog.Check2(addEssential(gadgetName, model.GadgetTrack(), snap.TypeGadget))

		// not skipped
		if gadget != nil {

			// always make sure the base of gadget is installed first
			info := mylog.Check2(readInfo(gadget.Path, gadget.SideInfo))

			gadgetBase := info.Base
			if gadgetBase == "" {
				gadgetBase = "core"
			}
			// Validity check
			// TODO: do we want to relax this? the new logic would allow
			// but it might just be confusing for now
			if baseSnap != "" && gadgetBase != baseSnap {
				return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", gadgetBase, model.Base())
			}
			mylog.Check2(addEssential(gadgetBase, "", snap.TypeBase))

		}
	}

	s.essentialSnapsNum = len(s.snaps)

	return nil
}

func (s *seed16) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	return s.LoadEssentialMetaWithSnapHandler(essentialTypes, nil, tm)
}

func (s *seed16) LoadEssentialMetaWithSnapHandler(essentialTypes []snap.Type, handler SnapHandler, tm timings.Measurer) error {
	model := s.Model()
	mylog.Check(s.loadYaml())

	required := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
	added := make(map[string]bool, 3)

	s.resetSnaps()

	if handler == nil {
		handler = defaultSnapHandler{}
	}

	if len(essentialTypes) == 0 {
		essentialTypes = nil
	}
	return s.loadEssentialMeta(essentialTypes, required, handler, added, tm)
}

func (s *seed16) LoadMeta(mode string, handler SnapHandler, tm timings.Measurer) error {
	if mode != AllModes && mode != "run" {
		return fmt.Errorf("internal error: Core 16/18 have only run mode, got: %s", mode)
	}

	model := s.Model()
	mylog.Check(s.loadYaml())

	required := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
	added := make(map[string]bool, 3)

	s.resetSnaps()

	if handler == nil {
		handler = defaultSnapHandler{}
	}
	mylog.Check(s.loadEssentialMeta(nil, required, handler, added, tm))

	// the rest of the snaps
	for _, sn := range s.yamlSnaps {
		if added[sn.Name] {
			continue
		}
		seedSnap := mylog.Check2(s.addSnap(sn, "", "", handler, nil, tm))

		if required.Contains(seedSnap) {
			seedSnap.Required = true
		}
	}

	return nil
}

func (s *seed16) UsesSnapdSnap() bool {
	return s.usesSnapdSnap
}

func (s *seed16) EssentialSnaps() []*Snap {
	return s.snaps[:s.essentialSnapsNum]
}

func (s *seed16) ModeSnaps(mode string) ([]*Snap, error) {
	if mode != "run" {
		return nil, fmt.Errorf("internal error: Core 16/18 have only run mode, got: %s", mode)
	}
	return s.snaps[s.essentialSnapsNum:], nil
}

func (s *seed16) NumSnaps() int {
	return len(s.snaps)
}

func (s *seed16) Iter(f func(sn *Snap) error) error {
	for _, sn := range s.snaps {
		mylog.Check(f(sn))
	}
	return nil
}
