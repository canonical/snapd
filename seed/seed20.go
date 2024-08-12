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

package seed

/* ATTN this should *not* use:

* dirs package: it is passed an explicit directory to work on

* release.OnClassic: it assumes classic based on the model classic
  option; consistency between system and model can/must be enforced
  elsewhere

*/

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

type seed20 struct {
	systemDir string

	db       asserts.RODatabase
	commitTo func(*asserts.Batch) error

	model *asserts.Model

	snapDeclsByID   map[string]*asserts.SnapDeclaration
	snapDeclsByName map[string]*asserts.SnapDeclaration

	snapRevsByID map[string]*asserts.SnapRevision

	nLoadMetaJobs int

	optSnaps    []*internal.Snap20
	optSnapsIdx int

	auxInfos map[string]*internal.AuxInfo20

	metaFilesLoaded bool

	snapsToConsiderCh chan snapToConsider

	essCache   map[string]*Snap
	essCacheMu sync.Mutex

	mode string

	snaps []*Snap
	// modes holds a matching applicable modes set for each snap in snaps
	modes             [][]string
	essentialSnapsNum int
}

// Copy implement Copier interface.
func (s *seed20) Copy(seedDir string, label string, tm timings.Measurer) (err error) {
	srcSystemDir, err := filepath.Abs(s.systemDir)
	if err != nil {
		return err
	}

	if label == "" {
		label = filepath.Base(srcSystemDir)
	}

	destSeedDir, err := filepath.Abs(seedDir)
	if err != nil {
		return err
	}

	if err := os.Mkdir(filepath.Join(destSeedDir, "systems"), 0755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}

	destSystemDir := filepath.Join(destSeedDir, "systems", label)
	if osutil.FileExists(destSystemDir) {
		return fmt.Errorf("cannot create system: system %q already exists at %q", label, destSystemDir)
	}

	// note: we don't clean up asserted snaps that were copied over
	defer func() {
		if err != nil {
			os.RemoveAll(destSystemDir)
		}
	}()

	if err := s.LoadMeta(AllModes, nil, tm); err != nil {
		return err
	}

	span := tm.StartSpan("copy-recovery-system", fmt.Sprintf("copy recovery system from %s to %s", srcSystemDir, destSystemDir))
	defer span.Stop()

	// copy all files (including unasserted snaps) from the seed to the
	// destination
	err = filepath.Walk(srcSystemDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		destPath := filepath.Join(destSeedDir, "systems", label, strings.TrimPrefix(path, srcSystemDir))
		if info.IsDir() {
			return os.Mkdir(destPath, info.Mode())
		}

		return osutil.CopyFile(path, destPath, osutil.CopyFlagDefault)
	})
	if err != nil {
		return err
	}

	destAssertedSnapDir := filepath.Join(destSeedDir, "snaps")

	if err := os.MkdirAll(destAssertedSnapDir, 0755); err != nil {
		return err
	}

	// copy the asserted snaps that the seed needs
	for _, sn := range s.snaps {
		// unasserted snaps are already copied above, skip them
		if sn.ID() == "" {
			continue
		}

		destSnapPath := filepath.Join(destAssertedSnapDir, filepath.Base(sn.Path))

		if err := osutil.CopyFile(sn.Path, destSnapPath, osutil.CopyFlagOverwrite); err != nil {
			return fmt.Errorf("cannot copy asserted snap: %w", err)
		}
	}

	return nil
}

func (s *seed20) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	if db == nil {
		// a db was not provided, create an internal temporary one
		var err error
		db, commitTo, err = newMemAssertionsDB(nil)
		if err != nil {
			return err
		}
	}

	assertsDir := filepath.Join(s.systemDir, "assertions")
	// collect assertions that are not the model
	var declRefs []*asserts.Ref
	var revRefs []*asserts.Ref
	checkAssertion := func(ref *asserts.Ref) error {
		switch ref.Type {
		case asserts.ModelType:
			return fmt.Errorf("system cannot have any model assertion but the one in the system model assertion file")
		case asserts.SnapDeclarationType:
			declRefs = append(declRefs, ref)
		case asserts.SnapRevisionType:
			revRefs = append(revRefs, ref)
		}
		return nil
	}

	batch, err := loadAssertions(assertsDir, checkAssertion)
	if err != nil {
		return err
	}

	refs, err := readAsserts(batch, filepath.Join(s.systemDir, "model"))
	if err != nil {
		return fmt.Errorf("cannot read model assertion: %v", err)
	}
	if len(refs) != 1 || refs[0].Type != asserts.ModelType {
		return fmt.Errorf("system model assertion file must contain exactly the model assertion")
	}
	modelRef := refs[0]

	if len(declRefs) != len(revRefs) {
		return fmt.Errorf("system unexpectedly holds a different number of snap-declaration than snap-revision assertions")
	}

	// this also verifies the consistency of all of them
	if err := commitTo(batch); err != nil {
		return err
	}

	find := func(ref *asserts.Ref) (asserts.Assertion, error) {
		a, err := ref.Resolve(db.Find)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot find just accepted assertion %v: %v", ref, err)
		}
		return a, nil
	}

	a, err := find(modelRef)
	if err != nil {
		return err
	}
	modelAssertion := a.(*asserts.Model)

	snapDeclsByName := make(map[string]*asserts.SnapDeclaration, len(declRefs))
	snapDeclsByID := make(map[string]*asserts.SnapDeclaration, len(declRefs))

	for _, declRef := range declRefs {
		a, err := find(declRef)
		if err != nil {
			return err
		}
		snapDecl := a.(*asserts.SnapDeclaration)
		snapDeclsByID[snapDecl.SnapID()] = snapDecl
		if snapDecl1 := snapDeclsByName[snapDecl.SnapName()]; snapDecl1 != nil {
			return fmt.Errorf("cannot have multiple snap-declarations for the same snap-name: %s", snapDecl.SnapName())
		}
		snapDeclsByName[snapDecl.SnapName()] = snapDecl
	}

	snapRevsByID := make(map[string]*asserts.SnapRevision, len(revRefs))

	for _, revRef := range revRefs {
		a, err := find(revRef)
		if err != nil {
			return err
		}
		snapRevision := a.(*asserts.SnapRevision)
		snapRevision1 := snapRevsByID[snapRevision.SnapID()]
		if snapRevision1 != nil {
			if snapRevision1.SnapRevision() != snapRevision.SnapRevision() {
				return fmt.Errorf("cannot have multiple snap-revisions for the same snap-id: %s", snapRevision1.SnapID())
			}
		} else {
			snapRevsByID[snapRevision.SnapID()] = snapRevision
		}
	}

	// remember db for later use
	s.db = db
	// remember commitTo for LoadPreseedAssertion
	s.commitTo = commitTo
	// remember
	s.model = modelAssertion
	s.snapDeclsByID = snapDeclsByID
	s.snapDeclsByName = snapDeclsByName
	s.snapRevsByID = snapRevsByID

	return nil
}

func (s *seed20) Model() *asserts.Model {
	if s.model == nil {
		panic("internal error: model assertion unset (LoadAssertions not called)")
	}
	return s.model
}

func (s *seed20) Brand() (*asserts.Account, error) {
	return findBrand(s, s.db)
}

func (s *seed20) UsesSnapdSnap() bool {
	return true
}

func (s *seed20) loadOptions() error {
	if s.model.Grade() != asserts.ModelDangerous {
		// options.yaml is not supported for grade > dangerous
		return nil
	}
	optionsFn := filepath.Join(s.systemDir, "options.yaml")
	if !osutil.FileExists(optionsFn) {
		// missing
		return nil
	}
	options20, err := internal.ReadOptions20(optionsFn)
	if err != nil {
		return err
	}
	s.optSnaps = options20.Snaps
	return nil
}

func (s *seed20) nextOptSnap(modSnap *asserts.ModelSnap) (optSnap *internal.Snap20, done bool) {
	// we can merge model snaps and options snaps because
	// both seed20.go and writer.go follow the order:
	// system snap, model.EssentialSnaps(), model.SnapsWithoutEssential()
	if s.optSnapsIdx == len(s.optSnaps) {
		return nil, true
	}
	next := s.optSnaps[s.optSnapsIdx]
	if modSnap == nil || naming.SameSnap(next, modSnap) {
		s.optSnapsIdx++
		return next, false
	}
	return nil, false
}

func (s *seed20) loadAuxInfos() error {
	auxInfoFn := filepath.Join(s.systemDir, "snaps", "aux-info.json")
	if !osutil.FileExists(auxInfoFn) {
		// missing
		return nil
	}

	f, err := os.Open(auxInfoFn)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	if err := dec.Decode(&s.auxInfos); err != nil {
		return fmt.Errorf("cannot decode aux-info.json: %v", err)
	}
	return nil
}

type noSnapDeclarationError struct {
	snapRef naming.SnapRef
}

func (e *noSnapDeclarationError) Error() string {
	snapID := e.snapRef.ID()
	if snapID != "" {
		return fmt.Sprintf("cannot find snap-declaration for snap-id: %s", snapID)
	}
	return fmt.Sprintf("cannot find snap-declaration for snap name: %s", e.snapRef.SnapName())
}

func (s *seed20) lookupVerifiedRevision(snapRef naming.SnapRef, handler ContainerHandler, snapsDir string, tm timings.Measurer) (snapPath string, snapRev *asserts.SnapRevision, snapDecl *asserts.SnapDeclaration, err error) {
	snapID := snapRef.ID()
	if snapID != "" {
		snapDecl = s.snapDeclsByID[snapID]
		if snapDecl == nil {
			return "", nil, nil, &noSnapDeclarationError{snapRef}
		}
	} else {
		if s.model.Grade() != asserts.ModelDangerous {
			return "", nil, nil, fmt.Errorf("all system snaps must be identified by snap-id, missing for %q", snapRef.SnapName())
		}
		snapName := snapRef.SnapName()
		snapDecl = s.snapDeclsByName[snapName]
		if snapDecl == nil {
			return "", nil, nil, &noSnapDeclarationError{snapRef}
		}
		snapID = snapDecl.SnapID()
	}

	snapRev = s.snapRevsByID[snapID]
	if snapRev == nil {
		return "", nil, nil, fmt.Errorf("internal error: cannot find snap-revision for snap-id: %s", snapID)
	}

	snapName := snapDecl.SnapName()
	snapPath = filepath.Join(s.systemDir, snapsDir, fmt.Sprintf("%s_%d.snap", snapName, snapRev.SnapRevision()))

	fi, err := os.Stat(snapPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("cannot stat snap: %v", err)
	}

	if fi.Size() != int64(snapRev.SnapSize()) {
		return "", nil, nil, fmt.Errorf("cannot validate %q for snap %q (snap-id %q), wrong size", snapPath, snapName, snapID)
	}

	cpi := snap.MinimalSnapContainerPlaceInfo(snapName, snap.R(snapRev.SnapRevision()))
	newPath, snapSHA3_384, _, err := handler.HandleAndDigestAssertedContainer(cpi, snapPath, tm)
	if err != nil {
		return "", nil, nil, err
	}

	if snapSHA3_384 != snapRev.SnapSHA3_384() {
		return "", nil, nil, fmt.Errorf("cannot validate %q for snap %q (snap-id %q), hash mismatch with snap-revision", snapPath, snapName, snapID)

	}

	if newPath != "" {
		snapPath = newPath
	}

	if _, err := snapasserts.CrossCheckProvenance(snapName, snapRev, snapDecl, s.model, s.db); err != nil {
		return "", nil, nil, err
	}

	// we have an authorized snap-revision with matching hash for
	// the blob, double check that the snap metadata provenance is
	// as expected
	if err := snapasserts.CheckProvenanceWithVerifiedRevision(snapPath, snapRev); err != nil {
		return "", nil, nil, err
	}

	return snapPath, snapRev, snapDecl, nil
}

func (s *seed20) lookupSnap(snapRef naming.SnapRef, optSnap *internal.Snap20, channel string, handler ContainerHandler, snapsDir string, tm timings.Measurer) (*Snap, error) {
	if optSnap != nil && optSnap.Channel != "" {
		channel = optSnap.Channel
	}

	var path string
	var sideInfo *snap.SideInfo
	if optSnap != nil && optSnap.Unasserted != "" {
		path = filepath.Join(s.systemDir, "snaps", optSnap.Unasserted)
		info, err := readInfo(path, nil)
		if err != nil {
			return nil, fmt.Errorf("cannot read unasserted snap: %v", err)
		}

		pinfo := snap.MinimalSnapContainerPlaceInfo(info.SnapName(), snap.Revision{N: -1})
		newPath, err := handler.HandleUnassertedContainer(pinfo, path, tm)
		if err != nil {
			return nil, err
		}
		if newPath != "" {
			path = newPath
		}
		sideInfo = &snap.SideInfo{RealName: info.SnapName()}
		// suppress channel
		channel = ""
	} else {
		var err error
		timings.Run(tm, "derive-side-info", fmt.Sprintf("hash and derive side info for snap %q", snapRef.SnapName()), func(nested timings.Measurer) {
			var snapRev *asserts.SnapRevision
			var snapDecl *asserts.SnapDeclaration
			path, snapRev, snapDecl, err = s.lookupVerifiedRevision(snapRef, handler, snapsDir, tm)
			if err == nil {
				sideInfo = snapasserts.SideInfoFromSnapAssertions(snapDecl, snapRev)
			}
		})
		if err != nil {
			return nil, err
		}
	}

	// complement with aux-info.json information
	auxInfo := s.auxInfos[sideInfo.SnapID]
	if auxInfo != nil {
		sideInfo.Private = auxInfo.Private
		sideInfo.EditedLinks = auxInfo.Links
		sideInfo.LegacyEditedContact = auxInfo.Contact
	}

	return &Snap{
		Path: path,

		SideInfo: sideInfo,

		Channel: channel,
	}, nil
}

type snapToConsider struct {
	// index of snap in seed20.snaps result slice
	index     int
	modelSnap *asserts.ModelSnap
	optSnap   *internal.Snap20
	// essential is set to true if the snap belongs to
	// Model.EssentialSnaps() which are shared across all modes
	essential bool
}

var errSkipped = errors.New("skipped optional snap")

func (s *seed20) doLoadMetaOne(sntoc *snapToConsider, handler ContainerHandler, tm timings.Measurer) (*Snap, error) {
	var snapRef naming.SnapRef
	var channel string
	var snapsDir string
	var essential bool
	var essType snap.Type
	var required bool
	var classic bool
	if sntoc.modelSnap != nil {
		snapRef = sntoc.modelSnap
		essential = sntoc.essential
		if essential {
			essType = snapTypeFromModel(sntoc.modelSnap)
		}
		required = essential || sntoc.modelSnap.Presence == "required"
		channel = sntoc.modelSnap.DefaultChannel
		classic = sntoc.modelSnap.Classic
		snapsDir = "../../snaps"
	} else {
		snapRef = sntoc.optSnap
		channel = "latest/stable"
		snapsDir = "snaps"
	}
	seedSnap, err := s.lookupSnap(snapRef, sntoc.optSnap, channel, handler, snapsDir, tm)
	if err != nil {
		if _, ok := err.(*noSnapDeclarationError); ok && !required {
			// skipped optional snap is ok
			return nil, errSkipped
		}
		return nil, err
	}
	seedSnap.Essential = essential
	seedSnap.Required = required
	seedSnap.Classic = classic
	if essential {
		if sntoc.modelSnap.SnapType == "gadget" {
			// validity
			info, err := readInfo(seedSnap.Path, seedSnap.SideInfo)
			if err != nil {
				return nil, err
			}
			if info.Base != s.model.Base() {
				return nil, fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, s.model.Base())
			}
			// TODO: when we allow extend models for classic
			// we need to add the gadget base here
		}

		seedSnap.EssentialType = essType
	}
	return seedSnap, nil
}

func (s *seed20) doLoadMeta(handler ContainerHandler, tm timings.Measurer) error {
	var cacheEssential func(snType string, essSnap *Snap)
	var cachedEssential func(snType string) *Snap
	if handler != nil {
		// ignore caching if not using the default handler
		// otherwise it would not always be called which could
		// be unexpected
		cacheEssential = func(string, *Snap) {}
		cachedEssential = func(string) *Snap { return nil }
	} else {
		handler = defaultSnapHandler{}
		// setup essential snaps cache
		if s.essCache == nil {
			// 4 = snapd+base+kernel+gadget
			s.essCache = make(map[string]*Snap, 4)
		}
		cacheEssential = func(snType string, essSnap *Snap) {
			s.essCacheMu.Lock()
			defer s.essCacheMu.Unlock()
			s.essCache[snType] = essSnap
		}
		cachedEssential = func(snType string) *Snap {
			s.essCacheMu.Lock()
			defer s.essCacheMu.Unlock()
			return s.essCache[snType]
		}
	}
	runMode := []string{"run"}

	// relevant snaps have now been queued in the channel
	n := len(s.snapsToConsiderCh)
	close(s.snapsToConsiderCh)
	if n > 0 {
		s.snaps = make([]*Snap, n)
		s.modes = make([][]string, n)
	}

	njobs := s.nLoadMetaJobs
	if njobs < 1 {
		njobs = 1
	}
	stopCh := make(chan struct{})
	outcomesCh := make(chan error, njobs)
	for j := 1; j <= njobs; j++ {
		jtm := tm.StartSpan(fmt.Sprintf("do-load-meta[%d]", j), fmt.Sprintf("snap metadata loading job #%d", j))
		go func() {
			var jobErr error
			// defers are LIFO, make sure that time snap is stopped
			// before we let the parent know that the goroutine is
			// done
			defer func() { outcomesCh <- jobErr }()
			defer jtm.Stop()
		Consider:
			for sntoc := range s.snapsToConsiderCh {
				select {
				case <-stopCh:
					break Consider
				default:
				}
				var seedSnap *Snap
				modes := runMode
				essential := false
				if sntoc.modelSnap != nil {
					modes = sntoc.modelSnap.Modes
					essential = sntoc.essential
				}
				if essential {
					seedSnap = cachedEssential(sntoc.modelSnap.SnapType)
				}
				if seedSnap == nil {
					var err error
					seedSnap, err = s.doLoadMetaOne(&sntoc, handler, jtm)
					if err != nil {
						if err == errSkipped {
							continue
						}
						jobErr = err
						return
					}
					if essential {
						cacheEssential(sntoc.modelSnap.SnapType, seedSnap)
					}
				}
				i := sntoc.index
				s.snaps[i] = seedSnap
				s.modes[i] = modes
			}
		}()
	}
	var firstErr error
	done := 0
	for done != njobs {
		err := <-outcomesCh
		done++
		if err != nil && firstErr == nil {
			// we will report the first encountered error
			// and do a best-effort to stop other jobs via stopCh
			firstErr = err
			close(stopCh)
		}
	}
	s.snapsToConsiderCh = nil
	if firstErr != nil {
		return firstErr
	}
	// filter out nil values from skipped snaps
	osnaps := s.snaps
	omodes := s.modes
	s.snaps = s.snaps[:0]
	s.modes = s.modes[:0]
	for i, sn := range osnaps {
		if sn != nil {
			s.snaps = append(s.snaps, sn)
			s.modes = append(s.modes, omodes[i])
		}
	}
	return nil
}

func (s *seed20) SetParallelism(n int) {
	s.nLoadMetaJobs = n
}

func (s *seed20) considerModelSnap(modelSnap *asserts.ModelSnap, essential bool, filter func(*asserts.ModelSnap) bool) {
	optSnap, _ := s.nextOptSnap(modelSnap)
	if filter != nil && !filter(modelSnap) {
		return
	}

	s.snapsToConsiderCh <- snapToConsider{
		index:     len(s.snapsToConsiderCh),
		modelSnap: modelSnap,
		optSnap:   optSnap,
		essential: essential,
	}

	if essential {
		s.essentialSnapsNum++
	}
}

func (s *seed20) LoadMeta(mode string, handler ContainerHandler, tm timings.Measurer) error {
	const otherSnapsFollow = true
	if err := s.queueEssentialMeta(nil, otherSnapsFollow, tm); err != nil {
		return err
	}
	s.mode = mode
	if err := s.queueModelRestMeta(tm); err != nil {
		return err
	}

	if s.mode == AllModes || s.mode == "run" {
		// extra snaps are only for run mode
		for {
			optSnap, done := s.nextOptSnap(nil)
			if done {
				break
			}

			s.snapsToConsiderCh <- snapToConsider{
				index:   len(s.snapsToConsiderCh),
				optSnap: optSnap,
			}
		}
	}

	return s.doLoadMeta(handler, tm)
}

func (s *seed20) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	return s.LoadEssentialMetaWithSnapHandler(essentialTypes, nil, tm)
}

func (s *seed20) LoadEssentialMetaWithSnapHandler(essentialTypes []snap.Type, handler ContainerHandler, tm timings.Measurer) error {
	var filterEssential func(*asserts.ModelSnap) bool
	if len(essentialTypes) != 0 {
		filterEssential = essentialSnapTypesToModelFilter(essentialTypes)
	}

	// only essential snaps
	const otherSnapsFollow = false
	if err := s.queueEssentialMeta(filterEssential, otherSnapsFollow, tm); err != nil {
		return err
	}

	err := s.doLoadMeta(handler, tm)
	if err != nil {
		return err
	}

	if len(essentialTypes) != 0 && s.essentialSnapsNum != len(essentialTypes) {
		// did not find all the explicitly asked essential types
		return fmt.Errorf("model does not specify all the requested essential snaps: %v", essentialTypes)
	}

	return nil
}

func (s *seed20) loadMetaFiles() error {
	if s.metaFilesLoaded {
		return nil
	}

	if err := s.loadOptions(); err != nil {
		return err
	}

	if err := s.loadAuxInfos(); err != nil {
		return err
	}

	s.metaFilesLoaded = true
	return nil
}

func (s *seed20) resetSnaps() {
	s.optSnapsIdx = 0
	s.mode = AllModes
	s.snaps = nil
	s.modes = nil
	s.essentialSnapsNum = 0
}

func (s *seed20) queueEssentialMeta(filterEssential func(*asserts.ModelSnap) bool, otherSnapsFollow bool, tm timings.Measurer) error {
	model := s.Model()

	if err := s.loadMetaFiles(); err != nil {
		return err
	}

	s.resetSnaps()

	essSnaps := model.EssentialSnaps()
	const essential = true

	// create queue channel
	m := len(essSnaps)
	if essSnaps[0].SnapType != "snapd" {
		m++
	}
	if otherSnapsFollow {
		m += len(model.SnapsWithoutEssential()) + len(s.optSnaps)
	}
	s.snapsToConsiderCh = make(chan snapToConsider, m)

	// an explicit snapd is the first of all of snaps
	if essSnaps[0].SnapType != "snapd" {
		snapdSnap := internal.MakeSystemSnap("snapd", "latest/stable", []string{"run", "ephemeral"})
		s.considerModelSnap(snapdSnap, essential, filterEssential)
	}

	for _, modelSnap := range essSnaps {
		s.considerModelSnap(modelSnap, essential, filterEssential)
	}

	return nil
}

func snapModesInclude(snapModes []string, mode string) bool {
	// mode is explicitly included in the snap modes
	if strutil.ListContains(snapModes, mode) {
		return true
	}
	if mode == "run" {
		// run is not an ephemeral mode (as all the others)
		// and it is not explicitly included in the snap modes
		return false
	}
	// mode is one of the ephemeral modes but was not included
	// explicitly in the snap modes, now check if the cover-all
	// "ephemeral" alias is included in the snap modes instead
	return strutil.ListContains(snapModes, "ephemeral")
}

func (s *seed20) queueModelRestMeta(tm timings.Measurer) error {
	model := s.Model()

	var filterMode func(*asserts.ModelSnap) bool
	if s.mode != AllModes {
		filterMode = func(modelSnap *asserts.ModelSnap) bool {
			return snapModesInclude(modelSnap.Modes, s.mode)
		}
	}

	const notEssential = false
	for _, modelSnap := range model.SnapsWithoutEssential() {
		s.considerModelSnap(modelSnap, notEssential, filterMode)
	}

	return nil
}

func (s *seed20) EssentialSnaps() []*Snap {
	return s.snaps[:s.essentialSnapsNum]
}

func (s *seed20) ModeSnaps(mode string) ([]*Snap, error) {
	if s.mode != AllModes && mode != s.mode {
		return nil, fmt.Errorf("metadata was loaded only for snaps for mode %s not %s", s.mode, mode)
	}
	snaps := s.snaps[s.essentialSnapsNum:]
	modes := s.modes[s.essentialSnapsNum:]
	nGuess := len(snaps)
	ephemeral := mode != "run"
	if ephemeral {
		nGuess /= 2
	}
	res := make([]*Snap, 0, nGuess)
	for i, snap := range snaps {
		if snapModesInclude(modes[i], mode) {
			res = append(res, snap)
		}
	}
	return res, nil
}

func (s *seed20) NumSnaps() int {
	return len(s.snaps)
}

func (s *seed20) Iter(f func(sn *Snap) error) error {
	for _, sn := range s.snaps {
		if err := f(sn); err != nil {
			return err
		}
	}
	return nil
}

func (s *seed20) LoadAutoImportAssertions(commitTo func(*asserts.Batch) error) error {
	if s.model.Grade() != asserts.ModelDangerous {
		return nil
	}

	autoImportAssert := filepath.Join(s.systemDir, "auto-import.assert")
	af, err := os.Open(autoImportAssert)
	if err != nil {
		return err
	}
	defer af.Close()
	batch := asserts.NewBatch(nil)
	if _, err := batch.AddStream(af); err != nil {
		return err
	}
	return commitTo(batch)
}

func (s *seed20) HasArtifact(relName string) bool {
	return osutil.FileExists(s.ArtifactPath(relName))
}

func (s *seed20) ArtifactPath(relName string) string {
	return filepath.Join(s.systemDir, relName)
}

func (s *seed20) LoadPreseedAssertion() (*asserts.Preseed, error) {
	model := s.Model()
	sysLabel := filepath.Base(s.systemDir)

	batch := asserts.NewBatch(nil)
	refs, err := readAsserts(batch, filepath.Join(s.systemDir, "preseed"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoPreseedAssertion
		}
	}
	var preseedRef *asserts.Ref
	for _, ref := range refs {
		if ref.Type == asserts.PreseedType {
			if preseedRef != nil {
				return nil, fmt.Errorf("system preseed assertion file cannot contain multiple preseed assertions")
			}
			preseedRef = ref
		}
	}
	if preseedRef == nil {
		return nil, fmt.Errorf("system preseed assertion file must contain a preseed assertion")
	}
	if err := s.commitTo(batch); err != nil {
		return nil, err
	}
	a, err := preseedRef.Resolve(s.db.Find)
	if err != nil {
		return nil, err
	}
	preseedAs := a.(*asserts.Preseed)

	if !strutil.ListContains(model.PreseedAuthority(), preseedAs.AuthorityID()) {
		return nil, fmt.Errorf("preseed authority-id %q is not allowed by the model", preseedAs.AuthorityID())
	}

	switch {
	case preseedAs.SystemLabel() != sysLabel:
		return nil, fmt.Errorf("preseed assertion system label %q doesn't match system label %q", preseedAs.SystemLabel(), sysLabel)
	case preseedAs.Model() != model.Model():
		return nil, fmt.Errorf("preseed assertion model %q doesn't match the model %q", preseedAs.Model(), model.Model())
	case preseedAs.BrandID() != model.BrandID():
		return nil, fmt.Errorf("preseed assertion brand %q doesn't match model brand %q", preseedAs.BrandID(), model.BrandID())
	case preseedAs.Series() != model.Series():
		return nil, fmt.Errorf("preseed assertion series %q doesn't match model series %q", preseedAs.Series(), model.Series())
	}
	return preseedAs, nil
}
