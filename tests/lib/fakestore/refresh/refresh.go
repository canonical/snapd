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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

func newAssertsDB() (*asserts.Database, error) {
	storePrivKey, _ := assertstest.ReadPrivKey(systestkeys.TestStorePrivKey)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Backstore:      asserts.NewMemoryBackstore(),
		Trusted:        sysdb.Trusted(),
	})
	if err != nil {
		return nil, err
	}
	// for signing
	db.ImportKey(storePrivKey)

	return db, nil
}

func MakeFakeRefreshForSnaps(snaps []string, blobDir string) error {
	db, err := newAssertsDB()
	if err != nil {
		return err
	}

	var cliConfig client.Config
	cli := client.New(&cliConfig)
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		headers := make(map[string]string)
		for i, k := range ref.Type.PrimaryKey {
			headers[k] = ref.PrimaryKey[i]
		}
		as, err := cli.Known(ref.Type.Name, headers)
		if err != nil {
			return nil, err
		}
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
		err := db.Add(a)
		if err != nil {
			if _, ok := err.(*asserts.RevisionError); !ok {
				return err
			}
		}
		_, err = writeAssert(a, blobDir)
		return err
	}

	f := asserts.NewFetcher(db, retrieve, save)

	for _, snap := range snaps {
		if err := makeFakeRefreshForSnap(snap, blobDir, db, f); err != nil {
			return err
		}
	}
	return nil
}

func writeAssert(a asserts.Assertion, targetDir string) (string, error) {
	ref := a.Ref()
	fn := fmt.Sprintf("%s.%s", strings.Join(ref.PrimaryKey, ","), ref.Type.Name)
	p := filepath.Join(targetDir, "asserts", fn)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return "", err
	}
	err := ioutil.WriteFile(p, asserts.Encode(a), 0644)
	return p, err
}

func makeFakeRefreshForSnap(snap, targetDir string, db *asserts.Database, f asserts.Fetcher) error {
	// make a fake update snap in /var/tmp (which is not a tempfs)
	fakeUpdateDir, err := ioutil.TempDir("/var/tmp", "snap-build-")
	if err != nil {
		return fmt.Errorf("creating tmp for fake update: %v", err)
	}
	// ensure the "." of the squashfs has sane owner/permissions
	err = exec.Command("sudo", "chown", "root:root", fakeUpdateDir).Run()
	if err != nil {
		return fmt.Errorf("changing owner of fake update dir: %v", err)
	}
	err = exec.Command("sudo", "chmod", "0755", fakeUpdateDir).Run()
	if err != nil {
		return fmt.Errorf("changing permissions of fake update dir: %v", err)
	}
	defer exec.Command("sudo", "rm", "-rf", fakeUpdateDir)

	origInfo, err := copySnap(snap, fakeUpdateDir)
	if err != nil {
		return fmt.Errorf("copying snap: %v", err)
	}

	err = copySnapAsserts(origInfo, f)
	if err != nil {
		return fmt.Errorf("copying asserts: %v", err)
	}

	// fake new version
	err = exec.Command("sudo", "sed", "-i", `s/version:\(.*\)/version:\1+fake1/`, filepath.Join(fakeUpdateDir, "meta/snap.yaml")).Run()
	if err != nil {
		return fmt.Errorf("changing fake snap version: %v", err)
	}

	newInfo, err := buildSnap(fakeUpdateDir, targetDir)
	if err != nil {
		return err
	}

	// new test-signed snap-revision
	err = makeNewSnapRevision(origInfo, newInfo, targetDir, db)
	if err != nil {
		return fmt.Errorf("cannot make new snap-revision: %v", err)
	}

	return nil
}

type info struct {
	revision string
	digest   string
	size     uint64
}

func copySnap(snapName, targetDir string) (*info, error) {
	baseDir := filepath.Join(dirs.SnapMountDir, snapName)
	if _, err := os.Stat(baseDir); err != nil {
		return nil, err
	}
	sourceDir := filepath.Join(baseDir, "current")
	files, err := filepath.Glob(filepath.Join(sourceDir, "*"))
	if err != nil {
		return nil, err
	}

	revnoDir, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return nil, err
	}
	origRevision := filepath.Base(revnoDir)

	for _, m := range files {
		if err = exec.Command("sudo", "cp", "-a", m, targetDir).Run(); err != nil {
			return nil, err

		}
	}

	rev, err := snap.ParseRevision(origRevision)
	if err != nil {
		return nil, err
	}

	place := snap.MinimalPlaceInfo(snapName, rev)
	origDigest, origSize, err := asserts.SnapFileSHA3_384(place.MountFile())
	if err != nil {
		return nil, err
	}

	return &info{revision: origRevision, size: origSize, digest: origDigest}, nil
}

func buildSnap(snapDir, targetDir string) (*info, error) {
	// build in /var/tmp (which is not a tempfs)
	cmd := exec.Command("snap", "pack", snapDir, targetDir)
	cmd.Env = append(os.Environ(), "TMPDIR=/var/tmp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("building fake snap: %v, output: %s", err, output)
	}
	out := strings.TrimSpace(string(output))
	if !strings.HasPrefix(out, "built: ") {
		return nil, fmt.Errorf("building fake snap got unexpected output: %s", output)
	}
	fn := out[len("built: "):]

	newDigest, size, err := asserts.SnapFileSHA3_384(fn)
	if err != nil {
		return nil, err
	}

	return &info{digest: newDigest, size: size}, nil
}

func copySnapAsserts(info *info, f asserts.Fetcher) error {
	return snapasserts.FetchSnapAssertions(f, info.digest)
}

func makeNewSnapRevision(orig, new *info, targetDir string, db *asserts.Database) error {
	a, err := db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": orig.digest,
	})
	if err != nil {
		return err
	}
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
	a, err = db.Sign(asserts.SnapRevisionType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return err
	}

	_, err = writeAssert(a, targetDir)
	return err
}
