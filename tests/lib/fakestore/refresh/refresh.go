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

func MakeFakeRefreshForSnaps(snap, blobDir, snapBlob, snapOrigBlob string) error {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return err
	}

	var cliConfig client.Config
	cli := client.New(&cliConfig)
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		headers, err := asserts.HeadersFromPrimaryKey(ref.Type, ref.PrimaryKey)
		if err != nil {
			return nil, err
		}
		as, err := cli.Known(ref.Type.Name, headers, nil)
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

	if err := makeFakeRefreshForSnap(snap, blobDir, snapBlob, snapOrigBlob, db, f); err != nil {
		return err
	}
	return nil
}

func writeAssert(a asserts.Assertion, targetDir string) (string, error) {
	ref := a.Ref()
	fn := fmt.Sprintf("%s.%s", strings.Join(asserts.ReducePrimaryKey(ref.Type, ref.PrimaryKey), ","), ref.Type.Name)
	p := filepath.Join(targetDir, "asserts", fn)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return "", err
	}
	err := os.WriteFile(p, asserts.Encode(a), 0644)
	return p, err
}

func makeFakeRefreshForSnap(snap, targetDir, snapBlob, snapOrigBlob string, db *asserts.Database, f asserts.Fetcher) error {
	// make a fake update snap in /var/tmp (which is not a tempfs)
	fakeUpdateDir, err := os.MkdirTemp("/var/tmp", "snap-build-")
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

	origInfo, err := getOrigInfo(snap, snapOrigBlob)
	if err != nil {
		return err
	}
	if snapBlob != "" {
		fi, err := os.Stat(snapBlob)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if err := copyDir(snapBlob, fakeUpdateDir); err != nil {
				return fmt.Errorf("copying snap blob dir: %v", err)
			}
		} else {
			if err := unpackSnap(snapBlob, fakeUpdateDir); err != nil {
				return fmt.Errorf("unpacking snap blob: %v", err)
			}
		}
	} else {
		if err := copySnap(snap, fakeUpdateDir); err != nil {
			return fmt.Errorf("copying snap: %v", err)
		}
	}

	err = copySnapAsserts(origInfo, f)
	if err != nil {
		return fmt.Errorf("copying asserts: %v", err)
	}

	// fake new version
	err = exec.Command("sudo", "sed", "-i",
		// version can be all numbers thus making it ambiguous and
		// needing quoting, eg. version: '2021112', but since we're
		// adding +fake1 suffix, the resulting value will clearly be a
		// string, so have the regex strip quoting too
		`s/version:[ ]\+['"]\?\([-.a-zA-Z0-9]\+\)['"]\?/version: \1+fake1/`,
		filepath.Join(fakeUpdateDir, "meta/snap.yaml")).Run()
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

func getOrigInfo(snapName, snapOrigBlob string) (*info, error) {
	if exists, isRegular, _ := osutil.RegularFileExists(snapOrigBlob); exists && isRegular {
		origDigest, origSize, err := asserts.SnapFileSHA3_384(snapOrigBlob)
		if err != nil {
			return nil, err
		}
		// XXX: figre out revision?
		return &info{revision: "x1", size: origSize, digest: origDigest}, nil
	}

	origRevision, err := currentRevision(snapName)
	if err != nil {
		return nil, err
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

func currentRevision(snapName string) (string, error) {
	baseDir := filepath.Join(dirs.SnapMountDir, snapName)
	if _, err := os.Stat(baseDir); err != nil {
		return "", err
	}
	sourceDir := filepath.Join(baseDir, "current")
	revnoDir, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return "", err
	}
	origRevision := filepath.Base(revnoDir)
	return origRevision, nil
}

func copyDir(sourceDir, targetDir string) error {
	files, err := filepath.Glob(filepath.Join(sourceDir, "*"))
	if err != nil {
		return err
	}

	for _, m := range files {
		if err = exec.Command("sudo", "cp", "-a", m, targetDir).Run(); err != nil {
			return err

		}
	}
	return nil
}

func copySnap(snapName, targetDir string) error {
	baseDir := filepath.Join(dirs.SnapMountDir, snapName)
	if _, err := os.Stat(baseDir); err != nil {
		return err
	}
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
	// assume provenance is unset
	return snapasserts.FetchSnapAssertions(f, info.digest, "")
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
