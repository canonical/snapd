// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/timings"
)

var trusted = sysdb.Trusted()

func MockTrusted(mockTrusted []asserts.Assertion) (restore func()) {
	prevTrusted := trusted
	trusted = mockTrusted
	return func() {
		trusted = prevTrusted
	}
}

func newMemAssertionsDB(commitObserve func(verified asserts.Assertion)) (db *asserts.Database, commitTo func(*asserts.Batch) error, err error) {
	memDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	}))

	commitTo = func(b *asserts.Batch) error {
		return b.CommitToAndObserve(memDB, commitObserve, nil)
	}

	return memDB, commitTo, nil
}

func loadAssertions(assertsDir string, loadedFunc func(*asserts.Ref) error) (*asserts.Batch, error) {
	logger.Debugf("loading assertions from %s", assertsDir)
	dc := mylog.Check2(os.ReadDir(assertsDir))

	batch := asserts.NewBatch(nil)
	for _, fi := range dc {
		fn := filepath.Join(assertsDir, fi.Name())
		refs := mylog.Check2(readAsserts(batch, fn))

		if loadedFunc != nil {
			for _, ref := range refs {
				mylog.Check(loadedFunc(ref))
			}
		}
	}

	return batch, nil
}

func readAsserts(batch *asserts.Batch, fn string) ([]*asserts.Ref, error) {
	f := mylog.Check2(os.Open(fn))

	defer f.Close()
	return batch.AddStream(f)
}

func readInfo(snapPath string, si *snap.SideInfo) (*snap.Info, error) {
	snapf := mylog.Check2(snapfile.Open(snapPath))

	return snap.ReadInfoFromSnapFile(snapf, si)
}

func snapTypeFromModel(modSnap *asserts.ModelSnap) snap.Type {
	switch modSnap.SnapType {
	case "base":
		return snap.TypeBase
	case "core":
		return snap.TypeOS
	case "gadget":
		return snap.TypeGadget
	case "kernel":
		return snap.TypeKernel
	case "snapd":
		return snap.TypeSnapd
	default:
		return snap.TypeApp
	}
}

func essentialSnapTypesToModelFilter(essentialTypes []snap.Type) func(modSnap *asserts.ModelSnap) bool {
	m := make(map[string]bool, len(essentialTypes))
	for _, t := range essentialTypes {
		switch t {
		case snap.TypeBase:
			m["base"] = true
		case snap.TypeOS:
			m["core"] = true
		case snap.TypeGadget:
			m["gadget"] = true
		case snap.TypeKernel:
			m["kernel"] = true
		case snap.TypeSnapd:
			m["snapd"] = true
		}
	}

	return func(modSnap *asserts.ModelSnap) bool {
		return m[modSnap.SnapType]
	}
}

func findBrand(seed Seed, db asserts.RODatabase) (*asserts.Account, error) {
	a := mylog.Check2(db.Find(asserts.AccountType, map[string]string{
		"account-id": seed.Model().BrandID(),
	}))

	return a.(*asserts.Account), nil
}

type defaultSnapHandler struct{}

func (h defaultSnapHandler) HandleUnassertedSnap(name, path string, _ timings.Measurer) (string, error) {
	return path, nil
}

func (h defaultSnapHandler) HandleAndDigestAssertedSnap(name, path string, essType snap.Type, _ *asserts.SnapRevision, _ func(string, uint64) (snap.Revision, error), _ timings.Measurer) (string, string, uint64, error) {
	sha3_384, size := mylog.Check3(asserts.SnapFileSHA3_384(path))

	return path, sha3_384, size, err
}
