// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package refresh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func newAssertsDB(signingPrivKey string) (*asserts.Database, error) {
	storePrivKey, _ := assertstest.ReadPrivKey(signingPrivKey)
	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Backstore:      asserts.NewMemoryBackstore(),
		Trusted:        sysdb.Trusted(),
	}))

	// for signing
	db.ImportKey(storePrivKey)

	return db, nil
}

func MakeFakeRefreshForSnaps(snap, blobDir, snapBlob, snapOrigBlob string) error {
	db := mylog.Check2(newAssertsDB(systestkeys.TestStorePrivKey))

	var cliConfig client.Config
	cli := client.New(&cliConfig)
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		headers := mylog.Check2(asserts.HeadersFromPrimaryKey(ref.Type, ref.PrimaryKey))

		as := mylog.Check2(cli.Known(ref.Type.Name, headers, nil))

		switch len(as) {
		case 1:
			return as[0], nil
		case 0:
			return nil, &asserts.NotFoundError{Type: ref.Type, Headers: headers}
		default:
			panic(fmt.Sprintf("multiple assertions when retrieving by primary key: %v", ref))
		}
	}

	save := func(a asserts.Assertion) error {
		mylog.Check(db.Add(a))

		_ = mylog.Check2(writeAssert(a, blobDir))
		return err
	}

	f := asserts.NewFetcher(db, retrieve, save)
	mylog.Check(makeFakeRefreshForSnap(snap, blobDir, snapBlob, snapOrigBlob, db, f))

	return nil
}

func writeAssert(a asserts.Assertion, targetDir string) (string, error) {
	ref := a.Ref()
	fn := fmt.Sprintf("%s.%s", strings.Join(asserts.ReducePrimaryKey(ref.Type, ref.PrimaryKey), ","), ref.Type.Name)
	p := filepath.Join(targetDir, "asserts", fn)
	mylog.Check(os.MkdirAll(filepath.Dir(p), 0755))
	mylog.Check(os.WriteFile(p, asserts.Encode(a), 0644))
	return p, err
}

func makeFakeRefreshForSnap(snap, targetDir, snapBlob, snapOrigBlob string, db *asserts.Database, f asserts.Fetcher) error {
	// make a fake update snap in /var/tmp (which is not a tempfs)
	fakeUpdateDir := mylog.Check2(os.MkdirTemp("/var/tmp", "snap-build-"))
	mylog.Check(

		// ensure the "." of the squashfs has sane owner/permissions
		exec.Command("sudo", "chown", "root:root", fakeUpdateDir).Run())
	mylog.Check(exec.Command("sudo", "chmod", "0755", fakeUpdateDir).Run())

	defer exec.Command("sudo", "rm", "-rf", fakeUpdateDir)

	origInfo := mylog.Check2(getOrigInfo(snap, snapOrigBlob))

	if snapBlob != "" {
		fi := mylog.Check2(os.Stat(snapBlob))

		if fi.IsDir() {
			mylog.Check(copyDir(snapBlob, fakeUpdateDir))
		} else {
			mylog.Check(unpackSnap(snapBlob, fakeUpdateDir))
		}
	} else {
		mylog.Check(copySnap(snap, fakeUpdateDir))
	}
	mylog.Check(copySnapAsserts(origInfo, f))
	mylog.Check(

		// fake new version
		exec.Command("sudo", "sed", "-i",
			// version can be all numbers thus making it ambiguous and
			// needing quoting, eg. version: '2021112', but since we're
			// adding +fake1 suffix, the resulting value will clearly be a
			// string, so have the regex strip quoting too
			`s/version:[ ]\+['"]\?\([-.a-zA-Z0-9]\+\)['"]\?/version: \1+fake1/`,
			filepath.Join(fakeUpdateDir, "meta/snap.yaml")).Run())

	newInfo := mylog.Check2(buildSnap(fakeUpdateDir, targetDir))
	mylog.Check(

		// new test-signed snap-revision
		makeNewSnapRevision(origInfo, newInfo, targetDir, db))

	return nil
}

type info struct {
	revision string
	digest   string
	size     uint64
}

func getOrigInfo(snapName, snapOrigBlob string) (*info, error) {
	if exists, isRegular, _ := osutil.RegularFileExists(snapOrigBlob); exists && isRegular {
		origDigest, origSize := mylog.Check3(asserts.SnapFileSHA3_384(snapOrigBlob))

		// XXX: figre out revision?
		return &info{revision: "x1", size: origSize, digest: origDigest}, nil
	}

	origRevision := mylog.Check2(currentRevision(snapName))

	rev := mylog.Check2(snap.ParseRevision(origRevision))

	place := snap.MinimalPlaceInfo(snapName, rev)
	origDigest, origSize := mylog.Check3(asserts.SnapFileSHA3_384(place.MountFile()))

	return &info{revision: origRevision, size: origSize, digest: origDigest}, nil
}

func currentRevision(snapName string) (string, error) {
	baseDir := filepath.Join(dirs.SnapMountDir, snapName)
	mylog.Check2(os.Stat(baseDir))

	sourceDir := filepath.Join(baseDir, "current")
	revnoDir := mylog.Check2(filepath.EvalSymlinks(sourceDir))

	origRevision := filepath.Base(revnoDir)
	return origRevision, nil
}

func copyDir(sourceDir, targetDir string) error {
	files := mylog.Check2(filepath.Glob(filepath.Join(sourceDir, "*")))

	for _, m := range files {
		mylog.Check(exec.Command("sudo", "cp", "-a", m, targetDir).Run())
	}
	return nil
}

func copySnap(snapName, targetDir string) error {
	baseDir := filepath.Join(dirs.SnapMountDir, snapName)
	mylog.Check2(os.Stat(baseDir))

	sourceDir := filepath.Join(baseDir, "current")
	return copyDir(sourceDir, targetDir)
}

func unpackSnap(snapBlob, targetDir string) error {
	return exec.Command("sudo", "unsquashfs", "-d", targetDir, "-f", snapBlob).Run()
}

func buildSnap(snapDir, targetDir string) (*info, error) {
	// build in /var/tmp (which is not a tempfs)
	cmd := exec.Command("snap", "pack", snapDir, targetDir)
	cmd.Env = append(os.Environ(), "TMPDIR=/var/tmp")
	output := mylog.Check2(cmd.CombinedOutput())

	out := strings.TrimSpace(string(output))
	if !strings.HasPrefix(out, "built: ") {
		return nil, fmt.Errorf("building fake snap got unexpected output: %s", output)
	}
	fn := out[len("built: "):]

	newDigest, size := mylog.Check3(asserts.SnapFileSHA3_384(fn))

	return &info{digest: newDigest, size: size}, nil
}

func copySnapAsserts(info *info, f asserts.Fetcher) error {
	// assume provenance is unset
	return snapasserts.FetchSnapAssertions(f, info.digest, "")
}

func makeNewSnapRevision(orig, new *info, targetDir string, db *asserts.Database) error {
	a := mylog.Check2(db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": orig.digest,
	}))

	origSnapRev := a.(*asserts.SnapRevision)

	headers := map[string]interface{}{
		"authority-id":  "testrootorg",
		"snap-sha3-384": new.digest,
		"snap-id":       origSnapRev.SnapID(),
		"snap-size":     fmt.Sprintf("%d", new.size),
		"snap-revision": fmt.Sprintf("%d", origSnapRev.SnapRevision()+1),
		"developer-id":  origSnapRev.DeveloperID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	a = mylog.Check2(db.Sign(asserts.SnapRevisionType, headers, nil, systestkeys.TestStoreKeyID))

	_ = mylog.Check2(writeAssert(a, targetDir))
	return err
}
