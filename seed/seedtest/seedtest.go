// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

// TestingSeed helps setting up a populated testing seed.
type TestingSeed struct {
	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts

	SnapsDir string
	AssertsDir string
}

type cleaner interface {
	AddCleanup(func())
}

func (s *TestingSeed) SetupAsserts(storeBrandID string, cleaner cleaner) {
	s.StoreSigning = assertstest.NewStoreStack(storeBrandID, nil)
	cleaner.AddCleanup(sysdb.InjectTrusted(s.StoreSigning.Trusted))

	s.Brands = assertstest.NewSigningAccounts(s.StoreSigning)
}

func (s *TestingSeed) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string) (snapFname string, snapDecl *asserts.SnapDeclaration, snapRev *asserts.SnapRevision) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	snapName := info.SnapName()

	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, files)
	snapFname = filepath.Base(mockSnapFile)

	targetFile := filepath.Join(s.SnapsDir, snapFname)
	err = os.Rename(mockSnapFile, targetFile)
	c.Assert(err, IsNil)

	cleanedName := strings.Replace(snapName, "-", "", -1)
	snapID := (cleanedName + strings.Repeat("id", 16)[len(cleanedName):])
	declA, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"publisher-id": developerID,
		"snap-name":    snapName,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	sha3_384, size, err := asserts.SnapFileSHA3_384(targetFile)
	c.Assert(err, IsNil)

	revA, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": sha3_384,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       snapID,
		"developer-id":  developerID,
		"snap-revision": revision.String(),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	return snapFname, declA.(*asserts.SnapDeclaration), revA.(*asserts.SnapRevision)
}

func (s *TestingSeed) MakeModelAssertionChain(brandID, model string, extras ...map[string]interface{}) []asserts.Assertion {
	assertChain := []asserts.Assertion{}
	modelA := s.Brands.Model(brandID, model, extras...)

	assertChain = append(assertChain, s.Brands.Account(modelA.BrandID()))
	assertChain = append(assertChain, s.Brands.AccountKey(modelA.BrandID()))
	assertChain = append(assertChain, modelA)

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	assertChain = append(assertChain, storeAccountKey)
	return assertChain
}

func (s *TestingSeed) WriteAssertionsToFile(fn string, assertions []asserts.Assertion) {
	multifn := filepath.Join(s.AssertsDir, fn)
	f, err := os.Create(multifn)
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
