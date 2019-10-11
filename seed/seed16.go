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

package seed

/* ATTN this should *not* use:

* dirs package: it is passed an explicit directory to work on

* release.OnClassic: it assumes classic based on the model classic
  option; consistency between system and model can/must be enforced
  elsewhere

*/

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/timings"
)

type seed16 struct {
	seedDir string

	db asserts.RODatabase

	model *asserts.Model

	snaps             []*Snap
	essentialSnapsNum int

	usesSnapdSnap bool
}

func (s *seed16) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	if db == nil {
		// a db was not provided, create an internal temporary one
		var err error
		db, commitTo, err = newMemAssertionsDB()
		if err != nil {
			return err
		}
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

	batch, err := loadAssertions(assertSeedDir, checkForModel)
	if err != nil {
		return err
	}

	// verify we have one model assertion
	if modelRef == nil {
		return fmt.Errorf("seed must have a model assertion")
	}

	if err := commitTo(batch); err != nil {
		return err
	}

	a, err := modelRef.Resolve(db.Find)
	if err != nil {
		return fmt.Errorf("internal error: cannot find just added assertion %v: %v", modelRef, err)
	}

	// remember db for later use
	s.db = db
	s.model = a.(*asserts.Model)

	return nil
}

func (s *seed16) Model() (*asserts.Model, error) {
	if s.model == nil {
		return nil, fmt.Errorf("internal error: model assertion unset")
	}
	return s.model, nil
}

func (s *seed16) addSnap(sn *Snap16, tm timings.Measurer) (*Snap, error) {
	path := filepath.Join(s.seedDir, "snaps", sn.File)
	seedSnap := &Snap{
		Path: path,
		// TODO|XXX: make sure channel is right for pinned tracks
		Channel: sn.Channel,
		Classic: sn.Classic,
		DevMode: sn.DevMode,
	}

	var sideInfo snap.SideInfo
	if sn.Unasserted {
		sideInfo.RealName = sn.Name
	} else {
		var si *snap.SideInfo
		var err error
		timings.Run(tm, "derive-side-info", fmt.Sprintf("hash and derive side info for snap %q", sn.Name), func(nested timings.Measurer) {
			si, err = snapasserts.DeriveSideInfo(path, s.db)
		})
		if asserts.IsNotFound(err) {
			return nil, fmt.Errorf("cannot find signatures with metadata for snap %q (%q)", sn.Name, path)
		}
		if err != nil {
			return nil, err
		}
		sideInfo = *si
		sideInfo.Private = sn.Private
		sideInfo.Contact = sn.Contact
	}

	seedSnap.SideInfo = &sideInfo

	return seedSnap, nil
}

func (s *seed16) LoadMeta(tm timings.Measurer) error {
	model, err := s.Model()
	if err != nil {
		return err
	}

	seedYamlFile := filepath.Join(s.seedDir, "seed.yaml")
	if !osutil.FileExists(seedYamlFile) {
		return ErrNoMeta
	}

	seedYaml, err := ReadYaml(seedYamlFile)
	if err != nil {
		return err
	}
	yamlSnaps := seedYaml.Snaps

	required := naming.NewSnapSet(model.RequiredWithEssentialSnaps())
	seeding := make(map[string]*Snap16, len(yamlSnaps))
	for _, sn := range yamlSnaps {
		seeding[sn.Name] = sn
	}
	classic := model.Classic()
	_, s.usesSnapdSnap = seeding["snapd"]

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

	// + 4 is for essential snaps (base, snapd, kernel, gadget)
	errors := make(chan error, len(yamlSnaps)+4)
	snaps := make(map[string]*Snap, len(yamlSnaps)+4)
	var seedingOrder []string

	// find snap name in ordered snaps
	findOrderedSnapName := func(name string) bool {
		for _, n := range seedingOrder {
			if n == name {
				return true
			}
		}
		return false
	}

	// add the essential snaps
	addEssential := func(snapName string) (*Snap, error) {
		yamlSnap := seeding[snapName]
		if yamlSnap == nil {
			return nil, fmt.Errorf("essential snap %q required by the model is missing in the seed", snapName)
		}

		seedSnap, err := s.addSnap(yamlSnap, tm)
		if err != nil {
			return nil, err
		}

		seedSnap.Essential = true
		seedSnap.Required = true
		snaps[snapName] = seedSnap

		return seedSnap, nil
	}

	var wg sync.WaitGroup

	addEssentialAsync := func(snapName string, errors chan<- error) {
		defer wg.Done()
		_, err := addEssential(snapName)
		errors <- err
	}

	// if there are snaps to seed, core/base needs to be seeded too
	if len(yamlSnaps) != 0 {
		// ensure "snapd" snap is installed first
		if model.Base() != "" || classicWithSnapd {
			seedingOrder = append(seedingOrder, "snapd")
			wg.Add(1)
			go addEssentialAsync("snapd", errors)
		}
		if !classicWithSnapd {
			seedingOrder = append(seedingOrder, baseSnap)
			wg.Add(1)
			go addEssentialAsync(baseSnap, errors)
		}
	}

	if kernelName := model.Kernel(); kernelName != "" {
		seedingOrder = append(seedingOrder, kernelName)
		wg.Add(1)
		go addEssentialAsync(kernelName, errors)
	}

	if gadgetName := model.Gadget(); gadgetName != "" {
		// gadgets are small, it's ok to run them straight
		seedingOrder = append(seedingOrder, gadgetName)
		gadget, err := addEssential(gadgetName)
		if err != nil {
			return err
		}

		// always make sure the base of gadget is installed first
		snapf, err := snap.Open(gadget.Path)
		if err != nil {
			return err
		}
		info, err := snap.ReadInfoFromSnapFile(snapf, gadget.SideInfo)
		if err != nil {
			return err
		}
		gadgetBase := info.Base
		if gadgetBase == "" {
			gadgetBase = "core"
		}
		// Sanity check
		// TODO: do we want to relax this? the new logic would allow
		// but it might just be confusing for now
		if baseSnap != "" && gadgetBase != baseSnap {
			return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", gadgetBase, model.Base())
		}
		if !findOrderedSnapName(gadgetBase) {
			seedingOrder = append(seedingOrder, gadgetBase)
			wg.Add(1)
			go addEssentialAsync(gadgetBase, errors)
		}
	}

	s.essentialSnapsNum = len(seedingOrder)

	addSnapAsync := func(errors chan<- error, sn *Snap16, wg *sync.WaitGroup) {
		defer wg.Done()
		seedSnap, err := s.addSnap(sn, tm)
		errors <- err
		if err != nil {
			return
		}
		snaps[sn.Name] = seedSnap
		if required.Contains(seedSnap) {
			seedSnap.Required = true
		}
	}

	// the rest of the snaps
	for _, sn := range yamlSnaps {
		if !findOrderedSnapName(sn.Name) {
			seedingOrder = append(seedingOrder, sn.Name)
			wg.Add(1)
			go addSnapAsync(errors, sn, &wg)
		}
	}

	wg.Wait()
	close(errors)

	// if there was any error, return first one
	for e := range errors {
		if e != nil {
			return e
		}
	}

	// populate s.snaps in right order
	for _, sn := range seedingOrder {
		snap := snaps[sn]
		if snap == nil {
			return fmt.Errorf("missing snap %s to be added", sn)
		}
		s.snaps = append(s.snaps, snap)
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
