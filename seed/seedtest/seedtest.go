// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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

package seedtest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
)

// SeedSnaps helps creating snaps for a seed.
type SeedSnaps struct {
	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts

	snaps     map[string]string
	infos     map[string]*snap.Info
	compInfos map[string][]*snap.ComponentInfo
	comps     map[string][]store.SnapResourceResult

	snapAssertNow time.Time

	snapRevs map[string]*asserts.SnapRevision
	resRevs  map[string][]*asserts.SnapResourceRevision
	resPairs map[string][]*asserts.SnapResourcePair
}

// SetupAssertSigning initializes StoreSigning for storeBrandID and Brands.
func (ss *SeedSnaps) SetupAssertSigning(storeBrandID string) {
	ss.StoreSigning = assertstest.NewStoreStack(storeBrandID, nil)
	ss.Brands = assertstest.NewSigningAccounts(ss.StoreSigning)
}

func (ss *SeedSnaps) AssertedSnapID(snapName string) string {
	snapID := naming.WellKnownSnapID(snapName)
	if snapID != "" {
		return snapID
	}
	return snaptest.AssertedSnapID(snapName)
}

func (ss *SeedSnaps) snapAssertionNow() time.Time {
	if ss.snapAssertNow.IsZero() {
		return time.Now()
	}
	return ss.snapAssertNow
}

func (ss *SeedSnaps) SetSnapAssertionNow(t time.Time) {
	ss.snapAssertNow = t
}

func (ss *SeedSnaps) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string, dbs ...*asserts.Database) (*asserts.SnapDeclaration, *asserts.SnapRevision) {
	return ss.makeAssertedSnap(c, snapYaml, files, revision, nil, developerID, ss.StoreSigning.SigningDB, "", nil, dbs...)
}

func (ss *SeedSnaps) MakeAssertedSnapWithComps(c *C, snapYaml string, files [][]string, revision snap.Revision, compRevisions map[string]snap.Revision, developerID string, dbs ...*asserts.Database) (*asserts.SnapDeclaration, *asserts.SnapRevision) {
	return ss.makeAssertedSnap(c, snapYaml, files, revision, compRevisions, developerID, ss.StoreSigning.SigningDB, "", nil, dbs...)
}

func (ss *SeedSnaps) MakeAssertedDelegatedSnapWithComps(c *C, snapYaml string, files [][]string, revision snap.Revision, compRevisions map[string]snap.Revision, developerID, delegateID, revProvenance string, revisionAuthority map[string]interface{}, dbs ...*asserts.Database) (*asserts.SnapDeclaration, *asserts.SnapRevision) {
	return ss.makeAssertedSnap(c, snapYaml, files, revision, compRevisions, developerID, ss.Brands.Signing(delegateID), revProvenance, revisionAuthority, dbs...)
}

func (ss *SeedSnaps) makeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, compRevisions map[string]snap.Revision, developerID string, revSigning *assertstest.SigningDB, revProvenance string, revisionAuthority map[string]interface{}, dbs ...*asserts.Database) (*asserts.SnapDeclaration, *asserts.SnapRevision) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	snapName := info.SnapName()

	snapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, files)

	snapID := ss.AssertedSnapID(snapName)
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"publisher-id": developerID,
		"snap-name":    snapName,
		"timestamp":    ss.snapAssertionNow().UTC().Format(time.RFC3339),
	}
	if revisionAuthority != nil {
		headers["revision-authority"] = []interface{}{revisionAuthority}
	}
	declA, err := ss.StoreSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	sha3_384, size, err := asserts.SnapFileSHA3_384(snapFile)
	c.Assert(err, IsNil)

	revHeaders := map[string]interface{}{
		"authority-id":  revSigning.AuthorityID,
		"snap-sha3-384": sha3_384,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       snapID,
		"developer-id":  developerID,
		"snap-revision": revision.String(),
		"timestamp":     ss.snapAssertionNow().UTC().Format(time.RFC3339),
	}
	if revProvenance != "" {
		revHeaders["provenance"] = revProvenance
	}
	revA, err := revSigning.Sign(asserts.SnapRevisionType, revHeaders, nil, "")
	c.Assert(err, IsNil)

	if !revision.Unset() {
		info.SnapID = snapID
		info.Revision = revision
	}

	for _, db := range dbs {
		err := db.Add(declA)
		c.Assert(err, IsNil)
		err = db.Add(revA)
		c.Assert(err, IsNil)
	}

	if ss.snaps == nil {
		ss.snaps = make(map[string]string)
		ss.infos = make(map[string]*snap.Info)
		ss.compInfos = make(map[string][]*snap.ComponentInfo)
		ss.comps = make(map[string][]store.SnapResourceResult)
		ss.snapRevs = make(map[string]*asserts.SnapRevision)
		ss.resRevs = make(map[string][]*asserts.SnapResourceRevision)
		ss.resPairs = make(map[string][]*asserts.SnapResourcePair)
	}

	ss.snaps[snapName] = snapFile
	info.SideInfo.RealName = snapName
	ss.infos[snapName] = info
	snapDecl := declA.(*asserts.SnapDeclaration)
	snapRev := revA.(*asserts.SnapRevision)
	ss.snapRevs[snapName] = snapRev

	if len(compRevisions) == 0 {
		compRevisions = make(map[string]snap.Revision, len(info.Components))
		for compName := range info.Components {
			compRevisions[compName] = snap.R(77)
		}
	}
	// If passed as argument, make sure it is of the right size
	c.Assert(len(compRevisions), Equals, len(info.Components))

	resResults := make([]store.SnapResourceResult, 0, len(info.Components))
	cinfos := make([]*snap.ComponentInfo, 0, len(info.Components))
	resRevs := make([]*asserts.SnapResourceRevision, 0, len(info.Components))
	resPairs := make([]*asserts.SnapResourcePair, 0, len(info.Components))
	for _, comp := range info.Components {
		cref := naming.NewComponentRef(snapName, comp.Name)
		compFile := snaptest.MakeTestComponent(c, SampleSnapYaml[cref.String()])
		ss.snaps[cref.String()] = compFile

		sha3_384, size, err := asserts.SnapFileSHA3_384(compFile)
		c.Assert(err, IsNil)

		// Now add the resource revision assertion
		compRev := compRevisions[comp.Name]
		resRevHeads := map[string]interface{}{
			"authority-id":      revSigning.AuthorityID,
			"snap-id":           snapID,
			"resource-name":     comp.Name,
			"resource-sha3-384": sha3_384,
			"resource-size":     fmt.Sprintf("%d", size),
			"resource-revision": compRev.String(),
			"developer-id":      developerID,
			"timestamp":         ss.snapAssertionNow().UTC().Format(time.RFC3339),
		}
		if revProvenance != "" {
			resRevHeads["provenance"] = revProvenance
		}
		resRev, err := revSigning.Sign(asserts.SnapResourceRevisionType, resRevHeads, nil, "")
		c.Assert(err, IsNil)
		resRevs = append(resRevs, resRev.(*asserts.SnapResourceRevision))

		// and the resource pair revision
		resPairHeads := map[string]interface{}{
			"authority-id":      revSigning.AuthorityID,
			"snap-id":           snapID,
			"resource-name":     comp.Name,
			"resource-revision": compRev.String(),
			"snap-revision":     revision.String(),
			"developer-id":      developerID,
			"timestamp":         ss.snapAssertionNow().UTC().Format(time.RFC3339),
		}
		if revProvenance != "" {
			resPairHeads["provenance"] = revProvenance
		}
		resPair, err := revSigning.Sign(asserts.SnapResourcePairType, resPairHeads, nil, "")
		c.Assert(err, IsNil)
		resPairs = append(resPairs, resPair.(*asserts.SnapResourcePair))

		for _, db := range dbs {
			err := db.Add(resRev)
			c.Assert(err, IsNil)
			err = db.Add(resPair)
			c.Assert(err, IsNil)
		}

		resResults = append(resResults, store.SnapResourceResult{
			DownloadInfo: snap.DownloadInfo{
				Size:     int64(size),
				Sha3_384: sha3_384,
			},
			Type:     "component/" + string(comp.Type),
			Name:     comp.Name,
			Revision: compRev.N,
		})

		cinfo, err := snap.InfoFromComponentYaml([]byte(SampleSnapYaml[cref.String()]))
		c.Assert(err, IsNil)
		cinfo.ComponentSideInfo = *snap.NewComponentSideInfo(cref, compRev)
		cinfos = append(cinfos, cinfo)
	}
	ss.compInfos[snapName] = cinfos
	ss.comps[snapName] = resResults
	ss.resRevs[snapName] = resRevs
	ss.resPairs[snapName] = resPairs

	return snapDecl, snapRev
}

func (ss *SeedSnaps) MakeAssertedDelegatedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID, delegateID, revProvenance string, revisionAuthority map[string]interface{}, dbs ...*asserts.Database) (*asserts.SnapDeclaration, *asserts.SnapRevision) {
	return ss.makeAssertedSnap(c, snapYaml, files, revision, nil, developerID, ss.Brands.Signing(delegateID), revProvenance, revisionAuthority, dbs...)
}

func (ss *SeedSnaps) AssertedSnap(snapName string) (snapFile string) {
	return ss.snaps[snapName]
}

func (ss *SeedSnaps) AssertedSnapInfo(snapName string) *snap.Info {
	return ss.infos[snapName]
}

func (ss *SeedSnaps) AssertedSnapComponents(snapName string) []store.SnapResourceResult {
	return ss.comps[snapName]
}

func (ss *SeedSnaps) AssertedSnapRevision(snapName string) *asserts.SnapRevision {
	return ss.snapRevs[snapName]
}

func (ss *SeedSnaps) AssertedResourceRevision(snapName string) []*asserts.SnapResourceRevision {
	return ss.resRevs[snapName]
}

func (ss *SeedSnaps) AssertedResourcePair(snapName string) []*asserts.SnapResourcePair {
	return ss.resPairs[snapName]
}

func (ss *SeedSnaps) AssertedComponentInfos(snapName string) []*snap.ComponentInfo {
	return ss.compInfos[snapName]
}

// TestingSeed16 helps setting up a populated Core 16/18 testing seed.
type TestingSeed16 struct {
	SeedSnaps

	SeedDir string
}

func (s *TestingSeed16) SnapsDir() string {
	return filepath.Join(s.SeedDir, "snaps")
}

func (s *TestingSeed16) AssertsDir() string {
	return filepath.Join(s.SeedDir, "assertions")
}

func (s *TestingSeed16) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string) (snapFname string, snapDecl *asserts.SnapDeclaration, snapRev *asserts.SnapRevision) {
	decl, rev := s.SeedSnaps.MakeAssertedSnap(c, snapYaml, files, revision, developerID)

	snapFile := s.snaps[decl.SnapName()]

	snapFname = filepath.Base(snapFile)
	targetFile := filepath.Join(s.SnapsDir(), snapFname)
	err := os.Rename(snapFile, targetFile)
	c.Assert(err, IsNil)

	return snapFname, decl, rev
}

func (s *TestingSeed16) MakeModelAssertionChain(brandID, model string, extras ...map[string]interface{}) []asserts.Assertion {
	assertChain := []asserts.Assertion{}
	modelA := s.Brands.Model(brandID, model, extras...)

	assertChain = append(assertChain, s.Brands.Account(modelA.BrandID()))
	assertChain = append(assertChain, s.Brands.AccountKey(modelA.BrandID()))
	assertChain = append(assertChain, modelA)

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	assertChain = append(assertChain, storeAccountKey)
	return assertChain
}

func (s *TestingSeed16) WriteAssertions(fn string, assertions ...asserts.Assertion) {
	fn = filepath.Join(s.AssertsDir(), fn)
	WriteAssertions(fn, assertions...)
}

func WriteAssertions(fn string, assertions ...asserts.Assertion) {
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := asserts.NewEncoder(f)
	for _, a := range assertions {
		err := enc.Encode(a)
		if err != nil {
			panic(err)
		}
	}
}

func ReadAssertions(c *C, fn string) []asserts.Assertion {
	f, err := os.Open(fn)
	c.Assert(err, IsNil)

	var as []asserts.Assertion
	dec := asserts.NewDecoder(f)
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, IsNil)
		as = append(as, a)
	}

	return as
}

// TestingSeed20 helps setting up a populated Core 20 testing seed directory.
type TestingSeed20 struct {
	SeedSnaps

	SeedDir string
}

// MakeSeed creates the seed with given label and generates model assertions
func (s *TestingSeed20) MakeSeed(c *C, label, brandID, modelID string, modelHeaders map[string]interface{}, optSnaps []*seedwriter.OptionsSnap) *asserts.Model {
	model := s.Brands.Model(brandID, modelID, modelHeaders)

	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys(brandID)...)

	s.MakeSeedWithModel(c, label, model, optSnaps, nil)
	return model
}

func (s *TestingSeed20) MakeSeedWithLocalComponents(c *C, label, brandID, modelID string, modelHeaders map[string]interface{}, optSnaps []*seedwriter.OptionsSnap, compPathsBySnap map[string][]string) *asserts.Model {
	model := s.Brands.Model(brandID, modelID, modelHeaders)

	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys(brandID)...)

	s.MakeSeedWithModel(c, label, model, optSnaps, compPathsBySnap)
	return model
}

// MakeSeedWithModel creates the seed with given label for a given model
func (s *TestingSeed20) MakeSeedWithModel(c *C, label string, model *asserts.Model, optSnaps []*seedwriter.OptionsSnap, compPathsBySnap map[string][]string) {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.StoreSigning.Find)
	}
	retrieveSeq := func(seq *asserts.AtSequence) (asserts.Assertion, error) {
		if seq.Sequence <= 0 {
			hdrs, err := asserts.HeadersFromSequenceKey(seq.Type, seq.SequenceKey)
			if err != nil {
				return nil, err
			}
			return s.StoreSigning.FindSequence(seq.Type, hdrs, -1, seq.Type.MaxSupportedFormat())
		}
		return seq.Resolve(s.StoreSigning.Find)
	}
	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		save2 := func(a asserts.Assertion) error {
			// for checking
			err := db.Add(a)
			if err != nil {
				if _, ok := err.(*asserts.RevisionError); ok {
					return nil
				}
				return err
			}
			return save(a)
		}
		return asserts.NewSequenceFormingFetcher(db, retrieve, retrieveSeq, save2)
	}
	sf := seedwriter.MakeSeedAssertionFetcher(newFetcher)

	opts := seedwriter.Options{
		SeedDir: s.SeedDir,
		Label:   label,
	}
	w, err := seedwriter.New(model, &opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps(optSnaps)
	c.Assert(err, IsNil)

	err = w.Start(db, sf)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)

	localARefs := make(map[*seedwriter.SeedSnap][]*asserts.Ref)

	for _, sn := range localSnaps {
		si, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, model, sf, db)
		if !errors.Is(err, &asserts.NotFoundError{}) {
			c.Assert(err, IsNil)
		}
		f, err := snapfile.Open(sn.Path)
		c.Assert(err, IsNil)
		info, err := snap.ReadInfoFromSnapFile(f, si)
		c.Assert(err, IsNil)

		// Add components from option paths
		compPaths := compPathsBySnap[info.SnapName()]
		seedComps := make(map[string]*seedwriter.SeedComponent, len(compPaths))
		for _, compPath := range compPaths {
			compf, err := snapfile.Open(compPath)
			c.Assert(err, IsNil)
			cinfo, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
			c.Assert(err, IsNil)
			if si != nil {
				// snap was asserted, components should be too
				csi, cRefs, err := seedwriter.DeriveComponentSideInfo(
					compPath, cinfo, info, model, sf, db)
				if !errors.Is(err, &asserts.NotFoundError{}) {
					c.Assert(err, IsNil)
					cinfo.ComponentSideInfo = *csi
				}
				aRefs = append(aRefs, cRefs...)
			}

			seedComps[cinfo.Component.ComponentName] = &seedwriter.SeedComponent{
				ComponentRef: cinfo.Component,
				Path:         compPath,
				Info:         cinfo,
			}
		}

		w.SetInfo(sn, info, seedComps)
		if aRefs != nil {
			localARefs[sn] = aRefs
		}
	}

	err = w.InfoDerived()
	c.Assert(err, IsNil)

	fetchAsserts := func(sn, _, _ *seedwriter.SeedSnap) ([]*asserts.Ref, error) {
		if aRefs, ok := localARefs[sn]; ok {
			return aRefs, nil
		}
		prev := len(sf.Refs())
		if err = sf.Save(s.snapRevs[sn.SnapName()]); err != nil {
			return nil, err
		}

		seededResources := make(map[string]bool)
		for _, comp := range sn.Components {
			seededResources[comp.ComponentName] = true
		}

		// Components assertions
		for _, resRev := range s.resRevs[sn.SnapName()] {
			if _, ok := seededResources[resRev.ResourceName()]; !ok {
				continue
			}

			if err = sf.Save(resRev); err != nil {
				return nil, err
			}
		}
		for _, resPair := range s.resPairs[sn.SnapName()] {
			if _, ok := seededResources[resPair.ResourceName()]; !ok {
				continue
			}

			if err = sf.Save(resPair); err != nil {
				return nil, err
			}
		}

		return sf.Refs()[prev:], nil
	}

	for {
		snaps, err := w.SnapsToDownload()
		c.Assert(err, IsNil)

		for _, sn := range snaps {
			name := sn.SnapName()

			info := s.AssertedSnapInfo(name)
			c.Assert(info, NotNil, Commentf("no snap info for %q", name))
			seedComps := make(map[string]*seedwriter.SeedComponent, len(sn.Components))
			cinfos := s.AssertedComponentInfos(name)
			for _, cc := range sn.Components {
				comp := cc
				found := false
				for _, ci := range cinfos {
					if ci.Component.ComponentName == comp.ComponentName {
						comp.Info = ci
						found = true
						break
					}
				}
				c.Assert(found, Equals, true)
				seedComps[comp.ComponentName] = &comp
			}
			err := w.SetInfo(sn, info, seedComps)
			c.Assert(err, IsNil)

			if _, err := os.Stat(sn.Path); err == nil {
				// snap is already present
				continue
			}

			// Put snaps/components containers in the seed
			err = os.Rename(s.AssertedSnap(name), sn.Path)
			c.Assert(err, IsNil)
			for _, comp := range sn.Components {
				err = os.Rename(s.AssertedSnap(comp.String()), comp.Path)
				c.Assert(err, IsNil)
			}
		}

		complete, err := w.Downloaded(fetchAsserts)
		c.Assert(err, IsNil)
		if complete {
			break
		}
	}

	copySnap := func(name, src, dst string) error {
		return osutil.CopyFile(src, dst, 0)
	}

	err = w.SeedSnaps(copySnap)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)
}

func ValidateSeed(c *C, root, label string, usesSnapd bool, trusted []asserts.Assertion) seed.Seed {
	tm := &timings.Timings{}
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	})
	c.Assert(err, IsNil)
	commitTo := func(b *asserts.Batch) error {
		return b.CommitTo(db, nil)
	}

	sd, err := seed.Open(root, label)
	c.Assert(err, IsNil)

	err = sd.LoadAssertions(db, commitTo)
	c.Assert(err, IsNil)

	err = sd.LoadMeta(seed.AllModes, nil, tm)
	c.Assert(err, IsNil)

	// core18/core20 use the snapd snap, old core does not
	c.Check(sd.UsesSnapdSnap(), Equals, usesSnapd)
	if usesSnapd {
		// core*, kernel, gadget, snapd
		c.Check(sd.EssentialSnaps(), HasLen, 4)
	} else {
		// core, kernel, gadget
		c.Check(sd.EssentialSnaps(), HasLen, 3)
	}
	return sd
}

var goodUser = map[string]interface{}{
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foo@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model", "other-model"},
	"name":         "Boring Guy",
	"username":     "guy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

func WriteValidAutoImportAssertion(c *C, brands *assertstest.SigningAccounts, seedDir, sysLabel string, perm os.FileMode) {
	systemUsers := []map[string]interface{}{goodUser}
	// write system user assertion to the system seed root
	autoImportAssert := filepath.Join(seedDir, "systems", sysLabel, "auto-import.assert")
	f, err := os.OpenFile(autoImportAssert, os.O_CREATE|os.O_WRONLY, perm)
	c.Assert(err, IsNil)
	defer f.Close()
	enc := asserts.NewEncoder(f)
	c.Assert(enc, NotNil)

	for _, suMap := range systemUsers {
		systemUser, err := brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, IsNil)
		systemUser = systemUser.(*asserts.SystemUser)
		err = enc.Encode(systemUser)
		c.Assert(err, IsNil)
	}
}
