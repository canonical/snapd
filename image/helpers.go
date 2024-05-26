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

package image

// TODO: put these in appropriate package(s) once they are clarified a bit more

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// FetchAndCheckSnapAssertions fetches and cross checks the snap assertions
// matching the given snap file using the provided asserts.Fetcher and
// assertion database.
// The optional model assertion must be passed for full cross checks.
func FetchAndCheckSnapAssertions(snapPath string, info *snap.Info, model *asserts.Model, f asserts.Fetcher, db asserts.RODatabase) (*asserts.SnapDeclaration, error) {
	sha3_384, size := mylog.Check3(asserts.SnapFileSHA3_384(snapPath))

	expectedProv := info.Provenance()
	mylog.Check(
		// this assumes series "16"
		snapasserts.FetchSnapAssertions(f, sha3_384, expectedProv))

	// cross checks
	verifiedRev := mylog.Check2(snapasserts.CrossCheck(info.InstanceName(), sha3_384, expectedProv, size, &info.SideInfo, model, db))
	mylog.Check(snapasserts.CheckProvenanceWithVerifiedRevision(snapPath, verifiedRev))

	a := mylog.Check2(db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": info.SnapID,
	}))

	return a.(*asserts.SnapDeclaration), nil
}

// var so that it can be mocked for tests
var writeResolvedContent = writeResolvedContentImpl

// writeResolvedContent takes gadget.Info and the unpacked
// gadget/kernel snaps and outputs the resolved content from the
// {gadget,kernel}.yaml into a filesystem tree with the structure:
// <prepareImageDir>/resolved-content/<volume-name>/part<structure-nr>/...
//
// E.g.
// /tmp/prep-img/resolved-content/pi/part0/{config.txt,bootcode.bin,...}
func writeResolvedContentImpl(prepareDir string, info *gadget.Info, gadgetUnpackDir, kernelUnpackDir string) error {
	fullPrepareDir := mylog.Check2(filepath.Abs(prepareDir))

	targetDir := filepath.Join(fullPrepareDir, "resolved-content")

	opts := &gadget.LayoutOptions{
		GadgetRootDir: gadgetUnpackDir,
		KernelRootDir: kernelUnpackDir,
	}
	for volName, vol := range info.Volumes {
		pvol := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

		for i, ps := range pvol.LaidOutStructure {
			if !ps.HasFilesystem() {
				continue
			}
			mw := mylog.Check2(gadget.NewMountedFilesystemWriter(nil, &ps, nil))

			// ubuntu-image uses the "part{}" nomenclature
			dst := filepath.Join(targetDir, volName, fmt.Sprintf("part%d", i))
			// on UC20, ensure system-seed links back to the
			// <PrepareDir>/system-seed
			if ps.Role() == gadget.SystemSeed {
				uc20systemSeedDir := filepath.Join(fullPrepareDir, "system-seed")
				mylog.Check(os.MkdirAll(filepath.Dir(dst), 0755))
				mylog.Check(os.Symlink(uc20systemSeedDir, dst))

			}
			mylog.Check(mw.Write(dst, nil))

		}
	}

	return nil
}
