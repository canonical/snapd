// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

package backend_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type snapshotSuite struct {
	root      string
	restore   []func()
	tarPath   string
	isTesting bool
}

// silly wrappers to get better failure messages
type isTestingSuite struct{ snapshotSuite }
type noTestingSuite struct{ snapshotSuite }

var _ = check.Suite(&isTestingSuite{snapshotSuite{isTesting: true}})
var _ = check.Suite(&noTestingSuite{snapshotSuite{isTesting: false}})

// tie gocheck into testing
func TestSnapshot(t *testing.T) { check.TestingT(t) }

type tableT struct {
	dir     string
	name    string
	content string
}

func table(si snap.PlaceInfo, homeDir string) []tableT {
	return []tableT{
		{
			dir:     si.DataDir(),
			name:    "foo",
			content: "versioned system canary\n",
		}, {
			dir:     si.CommonDataDir(),
			name:    "bar",
			content: "common system canary\n",
		}, {
			dir:     si.UserDataDir(homeDir, nil),
			name:    "ufoo",
			content: "versioned user canary\n",
		}, {
			dir:     si.UserCommonDataDir(homeDir, nil),
			name:    "ubar",
			content: "common user canary\n",
		},
	}
}

func (s *snapshotSuite) SetUpTest(c *check.C) {
	s.root = c.MkDir()

	dirs.SetRootDir(s.root)

	si := snap.MinimalPlaceInfo("hello-snap", snap.R(42))

	for _, t := range table(si, filepath.Join(dirs.GlobalRootDir, "home/snapuser")) {
		c.Check(os.MkdirAll(t.dir, 0755), check.IsNil)
		c.Check(ioutil.WriteFile(filepath.Join(t.dir, t.name), []byte(t.content), 0644), check.IsNil)
	}

	cur, err := user.Current()
	c.Assert(err, check.IsNil)

	s.restore = append(s.restore, backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "snapuser" {
			return nil, user.UnknownUserError(username)
		}
		rv := *cur
		rv.Username = username
		rv.HomeDir = filepath.Join(dirs.GlobalRootDir, "home/snapuser")
		return &rv, nil
	}),
		backend.MockIsTesting(s.isTesting),
	)

	s.tarPath, err = exec.LookPath("tar")
	c.Assert(err, check.IsNil)
}

func (s *snapshotSuite) TearDownTest(c *check.C) {
	dirs.SetRootDir("")
	for _, restore := range s.restore {
		restore()
	}
}

func hashkeys(snapshot *client.Snapshot) (keys []string) {
	for k := range snapshot.SHA3_384 {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

func (s *snapshotSuite) TestLastSnapshotID(c *check.C) {
	// LastSnapshotSetID is happy without any snapshots
	setID, err := backend.LastSnapshotSetID()
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(0))

	// create snapshots dir and dummy snapshots
	os.MkdirAll(dirs.SnapshotsDir, os.ModePerm)
	for _, name := range []string{
		"9_some-snap-1.zip", "1234_not-a-snapshot", "12_other-snap.zip", "3_foo.zip",
	} {
		c.Assert(ioutil.WriteFile(filepath.Join(dirs.SnapshotsDir, name), []byte{}, 0644), check.IsNil)
	}
	setID, err = backend.LastSnapshotSetID()
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(12))
}

func (s *snapshotSuite) TestLastSnapshotIDErrorOnDirNames(c *check.C) {
	// we need snapshots dir, otherwise LastSnapshotSetID exits early.
	c.Assert(os.MkdirAll(dirs.SnapshotsDir, os.ModePerm), check.IsNil)

	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		return nil, fmt.Errorf("fail")
	})()
	setID, err := backend.LastSnapshotSetID()
	c.Assert(err, check.ErrorMatches, "fail")
	c.Check(setID, check.Equals, uint64(0))
}

func (s *snapshotSuite) TestIsSnapshotFilename(c *check.C) {
	tests := []struct {
		name  string
		valid bool
		setID uint64
	}{
		{"1_foo.zip", true, 1},
		{"14_hello-world_6.4_29.zip", true, 14},
		{"1_.zip", false, 0},
		{"1_foo.zip.bak", false, 0},
		{"foo_1_foo.zip", false, 0},
		{"foo_bar_baz.zip", false, 0},
		{"", false, 0},
		{"1_", false, 0},
	}

	for _, t := range tests {
		ok, setID := backend.IsSnapshotFilename(t.name)
		c.Check(ok, check.Equals, t.valid, check.Commentf("fail: %s", t.name))
		c.Check(setID, check.Equals, t.setID, check.Commentf("fail: %s", t.name))
	}
}

func (s *snapshotSuite) TestIterBailsIfContextDone(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, nil // deal with it
	})()

	err := backend.Iter(ctx, nil)
	c.Check(err, check.Equals, context.Canceled)
	c.Check(triedToOpenDir, check.Equals, false)
}

func (s *snapshotSuite) TestIterBailsIfContextDoneMidway(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return os.Open(os.DevNull)
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		cancel()
		return []string{"hello"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		return nil, nil
	})()

	err := backend.Iter(ctx, nil)
	c.Check(err, check.Equals, context.Canceled)
	c.Check(triedToOpenDir, check.Equals, true)
	// bails as soon as
	c.Check(readNames, check.Equals, 1)
	c.Check(triedToOpenSnapshot, check.Equals, false)
}

func (s *snapshotSuite) TestIterReturnsOkIfSnapshotsDirNonexistent(c *check.C) {
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, os.ErrNotExist
	})()

	err := backend.Iter(context.Background(), nil)
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
}

func (s *snapshotSuite) TestIterBailsIfSnapshotsDirFails(c *check.C) {
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, os.ErrInvalid
	})()

	err := backend.Iter(context.Background(), nil)
	c.Check(err, check.ErrorMatches, "cannot open snapshots directory: invalid argument")
	c.Check(triedToOpenDir, check.Equals, true)
}

func (s *snapshotSuite) TestIterWarnsOnOpenErrorIfSnapshotNil(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{"1_hello.zip"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		return nil, os.ErrInvalid
	})()

	calledF := false
	f := func(snapshot *backend.Reader) error {
		calledF = true
		return nil
	}

	err := backend.Iter(context.Background(), f)
	// snapshot open errors are not failures:
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 2)
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Matches, `(?m).* Cannot open snapshot "1_hello.zip": invalid argument.`)
	c.Check(calledF, check.Equals, false)
}

func (s *snapshotSuite) TestIterCallsFuncIfSnapshotNotNil(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{"1_hello.zip"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		// NOTE non-nil reader, and error, returned
		r := backend.Reader{}
		r.SetID = 1
		r.Broken = "xyzzy"
		return &r, os.ErrInvalid
	})()

	calledF := false
	f := func(snapshot *backend.Reader) error {
		c.Check(snapshot.Broken, check.Equals, "xyzzy")
		calledF = true
		return nil
	}

	err := backend.Iter(context.Background(), f)
	// snapshot open errors are not failures:
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 2)
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Equals, "")
	c.Check(calledF, check.Equals, true)
}

func (s *snapshotSuite) TestIterReportsCloseError(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockOsOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{"42_hello.zip"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string, uint64) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		r := backend.Reader{}
		r.SetID = 42
		return &r, nil
	})()

	calledF := false
	f := func(snapshot *backend.Reader) error {
		c.Check(snapshot.SetID, check.Equals, uint64(42))
		calledF = true
		return nil
	}

	err := backend.Iter(context.Background(), f)
	// snapshot close errors _are_ failures (because they're completely unexpected):
	c.Check(err, check.Equals, os.ErrInvalid)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 1) // never gets to read another one
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Equals, "")
	c.Check(calledF, check.Equals, true)
}

func readerForFilename(fname string, c *check.C) *backend.Reader {
	var snapname string
	var id uint64
	fn := strings.TrimSuffix(filepath.Base(fname), ".zip")
	_, err := fmt.Sscanf(fn, "%d_%s", &id, &snapname)
	c.Assert(err, check.IsNil, check.Commentf(fn))
	f, err := os.Open(os.DevNull)
	c.Assert(err, check.IsNil, check.Commentf(fn))
	return &backend.Reader{
		File: f,
		Snapshot: client.Snapshot{
			SetID: id,
			Snap:  snapname,
		},
	}
}

func (s *snapshotSuite) TestIterIgnoresSnapshotsWithInvalidNames(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	defer backend.MockOsOpen(func(string) (*os.File, error) {
		return new(os.File), nil
	})()
	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 1 {
			return nil, io.EOF
		}
		return []string{
			"_foo.zip",
			"43_bar.zip",
			"foo_bar.zip",
			"bar.",
		}, nil
	})()
	defer backend.MockOpen(func(fname string, setID uint64) (*backend.Reader, error) {
		return readerForFilename(fname, c), nil
	})()

	var calledF int
	f := func(snapshot *backend.Reader) error {
		calledF++
		c.Check(snapshot.SetID, check.Equals, uint64(43))
		return nil
	}

	err := backend.Iter(context.Background(), f)
	c.Check(err, check.IsNil)
	c.Check(logbuf.String(), check.Equals, "")
	c.Check(calledF, check.Equals, 1)
}

func (s *snapshotSuite) TestIterSetIDoverride(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}

	shw, err := backend.Save(context.TODO(), 12, info, cfg, []string{"snapuser"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, uint64(12))

	snapshotPath := filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip")
	c.Check(backend.Filename(shw), check.Equals, snapshotPath)
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	// rename the snapshot, verify that set id from the filename is used by the reader.
	c.Assert(os.Rename(snapshotPath, filepath.Join(dirs.SnapshotsDir, "33_hello.zip")), check.IsNil)

	var calledF int
	f := func(snapshot *backend.Reader) error {
		calledF++
		c.Check(snapshot.SetID, check.Equals, uint64(uint(33)))
		c.Check(snapshot.Snap, check.Equals, "hello-snap")
		return nil
	}

	c.Assert(backend.Iter(context.Background(), f), check.IsNil)
	c.Check(calledF, check.Equals, 1)
}

func (s *snapshotSuite) TestList(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	defer backend.MockOsOpen(func(string) (*os.File, error) { return new(os.File), nil })()

	readNames := 0
	defer backend.MockDirNames(func(*os.File, int) ([]string, error) {
		readNames++
		if readNames > 4 {
			return nil, io.EOF
		}
		return []string{
			fmt.Sprintf("%d_foo.zip", readNames),
			fmt.Sprintf("%d_bar.zip", readNames),
			fmt.Sprintf("%d_baz.zip", readNames),
		}, nil
	})()
	defer backend.MockOpen(func(fn string, setID uint64) (*backend.Reader, error) {
		var id uint64
		var snapname string
		c.Assert(strings.HasSuffix(fn, ".zip"), check.Equals, true)
		fn = strings.TrimSuffix(filepath.Base(fn), ".zip")
		_, err := fmt.Sscanf(fn, "%d_%s", &id, &snapname)
		c.Assert(err, check.IsNil, check.Commentf(fn))
		f, err := os.Open(os.DevNull)
		c.Assert(err, check.IsNil, check.Commentf(fn))
		return &backend.Reader{
			File: f,
			Snapshot: client.Snapshot{
				SetID:    id,
				Snap:     snapname,
				SnapID:   "id-for-" + snapname,
				Version:  "v1.0-" + snapname,
				Revision: snap.R(int(id)),
			},
		}, nil
	})()

	type tableT struct {
		setID     uint64
		snapnames []string
		numSets   int
		numShots  int
		predicate func(*client.Snapshot) bool
	}
	table := []tableT{
		{0, nil, 4, 12, nil},
		{0, []string{"foo"}, 4, 4, func(snapshot *client.Snapshot) bool { return snapshot.Snap == "foo" }},
		{1, nil, 1, 3, func(snapshot *client.Snapshot) bool { return snapshot.SetID == 1 }},
		{2, []string{"bar"}, 1, 1, func(snapshot *client.Snapshot) bool { return snapshot.Snap == "bar" && snapshot.SetID == 2 }},
		{0, []string{"foo", "bar"}, 4, 8, func(snapshot *client.Snapshot) bool { return snapshot.Snap == "foo" || snapshot.Snap == "bar" }},
	}

	for i, t := range table {
		comm := check.Commentf("%d: %d/%v", i, t.setID, t.snapnames)
		// reset
		readNames = 0
		logbuf.Reset()

		sets, err := backend.List(context.Background(), t.setID, t.snapnames)
		c.Check(err, check.IsNil, comm)
		c.Check(readNames, check.Equals, 5, comm)
		c.Check(logbuf.String(), check.Equals, "", comm)
		c.Check(sets, check.HasLen, t.numSets, comm)
		nShots := 0
		fnTpl := filepath.Join(dirs.SnapshotsDir, "%d_%s_%s_%s.zip")
		for j, ss := range sets {
			for k, snapshot := range ss.Snapshots {
				comm := check.Commentf("%d: %d/%v #%d/%d", i, t.setID, t.snapnames, j, k)
				if t.predicate != nil {
					c.Check(t.predicate(snapshot), check.Equals, true, comm)
				}
				nShots++
				fn := fmt.Sprintf(fnTpl, snapshot.SetID, snapshot.Snap, snapshot.Version, snapshot.Revision)
				c.Check(backend.Filename(snapshot), check.Equals, fn, comm)
				c.Check(snapshot.SnapID, check.Equals, "id-for-"+snapshot.Snap)
			}
		}
		c.Check(nShots, check.Equals, t.numShots)
	}
}

func (s *snapshotSuite) TestAddDirToZipBails(c *check.C) {
	snapshot := &client.Snapshot{SetID: 42, Snap: "a-snap"}
	buf, restore := logger.MockLogger()
	defer restore()
	// note as the zip is nil this would panic if it didn't bail
	c.Check(backend.AddDirToZip(nil, snapshot, nil, "", "an/entry", filepath.Join(s.root, "nonexistent")), check.IsNil)
	// no log for the non-existent case
	c.Check(buf.String(), check.Equals, "")
	buf.Reset()
	c.Check(backend.AddDirToZip(nil, snapshot, nil, "", "an/entry", "/etc/passwd"), check.IsNil)
	c.Check(buf.String(), check.Matches, "(?m).* is not a directory.")
}

func (s *snapshotSuite) TestAddDirToZipTarFails(c *check.C) {
	d := filepath.Join(s.root, "foo")
	c.Assert(os.MkdirAll(filepath.Join(d, "bar"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.root, "common"), 0755), check.IsNil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	c.Assert(backend.AddDirToZip(ctx, nil, z, "", "an/entry", d), check.ErrorMatches, ".* context canceled")
}

func (s *snapshotSuite) TestAddDirToZip(c *check.C) {
	d := filepath.Join(s.root, "foo")
	c.Assert(os.MkdirAll(filepath.Join(d, "bar"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.root, "common"), 0755), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "bar", "baz"), []byte("hello\n"), 0644), check.IsNil)

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	snapshot := &client.Snapshot{
		SHA3_384: map[string]string{},
	}
	c.Assert(backend.AddDirToZip(context.Background(), snapshot, z, "", "an/entry", d), check.IsNil)
	z.Close() // write out the central directory

	c.Check(snapshot.SHA3_384, check.HasLen, 1)
	c.Check(snapshot.SHA3_384["an/entry"], check.HasLen, 96)
	c.Check(snapshot.Size > 0, check.Equals, true) // actual size most likely system-dependent
	br := bytes.NewReader(buf.Bytes())
	r, err := zip.NewReader(br, int64(br.Len()))
	c.Assert(err, check.IsNil)
	c.Check(r.File, check.HasLen, 1)
	c.Check(r.File[0].Name, check.Equals, "an/entry")
}

func (s *snapshotSuite) TestHappyRoundtrip(c *check.C) {
	s.testHappyRoundtrip(c, "marker")
}

func (s *snapshotSuite) TestHappyRoundtripNoCommon(c *check.C) {
	for _, t := range table(snap.MinimalPlaceInfo("hello-snap", snap.R(42)), filepath.Join(dirs.GlobalRootDir, "home/snapuser")) {
		if _, d := filepath.Split(t.dir); d == "common" {
			c.Assert(os.RemoveAll(t.dir), check.IsNil)
		}
	}
	s.testHappyRoundtrip(c, "marker")
}

func (s *snapshotSuite) TestHappyRoundtripNoRev(c *check.C) {
	for _, t := range table(snap.MinimalPlaceInfo("hello-snap", snap.R(42)), filepath.Join(dirs.GlobalRootDir, "home/snapuser")) {
		if _, d := filepath.Split(t.dir); d == "42" {
			c.Assert(os.RemoveAll(t.dir), check.IsNil)
		}
	}
	s.testHappyRoundtrip(c, "../common/marker")
}

func (s *snapshotSuite) testHappyRoundtrip(c *check.C, marker string) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}
	shID := uint64(12)

	shw, err := backend.Save(context.TODO(), shID, info, cfg, []string{"snapuser"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)
	c.Check(shw.Snap, check.Equals, info.InstanceName())
	c.Check(shw.SnapID, check.Equals, info.SnapID)
	c.Check(shw.Version, check.Equals, info.Version)
	c.Check(shw.Epoch, check.DeepEquals, epoch)
	c.Check(shw.Revision, check.Equals, info.Revision)
	c.Check(shw.Conf, check.DeepEquals, cfg)
	c.Check(shw.Auto, check.Equals, false)
	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	shs, err := backend.List(context.TODO(), 0, nil)
	c.Assert(err, check.IsNil)
	c.Assert(shs, check.HasLen, 1)
	c.Assert(shs[0].Snapshots, check.HasLen, 1)

	shr, err := backend.Open(backend.Filename(shw), backend.ExtractFnameSetID)
	c.Assert(err, check.IsNil)
	defer shr.Close()

	for label, sh := range map[string]*client.Snapshot{"open": &shr.Snapshot, "list": shs[0].Snapshots[0]} {
		comm := check.Commentf("%q", label)
		c.Check(sh.SetID, check.Equals, shID, comm)
		c.Check(sh.Snap, check.Equals, info.InstanceName(), comm)
		c.Check(sh.SnapID, check.Equals, info.SnapID, comm)
		c.Check(sh.Version, check.Equals, info.Version, comm)
		c.Check(sh.Epoch, check.DeepEquals, epoch)
		c.Check(sh.Revision, check.Equals, info.Revision, comm)
		c.Check(sh.Conf, check.DeepEquals, cfg, comm)
		c.Check(sh.SHA3_384, check.DeepEquals, shw.SHA3_384, comm)
		c.Check(sh.Auto, check.Equals, false)
	}
	c.Check(shr.Name(), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(shr.Check(context.TODO(), nil), check.IsNil)

	newroot := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(newroot, "home/snapuser"), 0755), check.IsNil)
	dirs.SetRootDir(newroot)

	var diff = func() *exec.Cmd {
		cmd := exec.Command("diff", "-urN", "-x*.zip", s.root, newroot)
		// cmd.Stdout = os.Stdout
		// cmd.Stderr = os.Stderr
		return cmd
	}

	for i := 0; i < 3; i++ {
		comm := check.Commentf("%d", i)
		// sanity check
		c.Check(diff().Run(), check.NotNil, comm)

		// restore leaves things like they were (again and again)
		rs, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
		c.Assert(err, check.IsNil, comm)
		rs.Cleanup()
		c.Check(diff().Run(), check.IsNil, comm)

		// dirty it -> no longer like it was
		c.Check(ioutil.WriteFile(filepath.Join(info.DataDir(), marker), []byte("scribble\n"), 0644), check.IsNil, comm)
	}
}

func (s *snapshotSuite) TestOpenSetIDoverride(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}

	shw, err := backend.Save(context.TODO(), 12, info, cfg, []string{"snapuser"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, uint64(12))

	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	shr, err := backend.Open(backend.Filename(shw), 99)
	c.Assert(err, check.IsNil)
	defer shr.Close()

	c.Check(shr.SetID, check.Equals, uint64(99))
}

func (s *snapshotSuite) TestRestoreRoundtripDifferentRevision(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	shID := uint64(12)

	shw, err := backend.Save(context.TODO(), shID, info, nil, []string{"snapuser"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(shw.Revision, check.Equals, info.Revision)

	shr, err := backend.Open(backend.Filename(shw), backend.ExtractFnameSetID)
	c.Assert(err, check.IsNil)
	defer shr.Close()

	c.Check(shr.Revision, check.Equals, info.Revision)
	c.Check(shr.Name(), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))

	// move the expected data to its expected place
	for _, dir := range []string{
		filepath.Join(s.root, "home", "snapuser", "snap", "hello-snap"),
		filepath.Join(dirs.SnapDataDir, "hello-snap"),
	} {
		c.Check(os.Rename(filepath.Join(dir, "42"), filepath.Join(dir, "17")), check.IsNil)
	}

	newroot := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(newroot, "home", "snapuser"), 0755), check.IsNil)
	dirs.SetRootDir(newroot)

	var diff = func() *exec.Cmd {
		cmd := exec.Command("diff", "-urN", "-x*.zip", s.root, newroot)
		// cmd.Stdout = os.Stdout
		// cmd.Stderr = os.Stderr
		return cmd
	}

	// sanity check
	c.Check(diff().Run(), check.NotNil)

	// restore leaves things like they were, but in the new dir
	rs, err := shr.Restore(context.TODO(), snap.R("17"), nil, logger.Debugf, nil)
	c.Assert(err, check.IsNil)
	rs.Cleanup()
	c.Check(diff().Run(), check.IsNil)
}

func (s *snapshotSuite) TestPickUserWrapperRunuser(c *check.C) {
	n := 0
	defer backend.MockExecLookPath(func(s string) (string, error) {
		n++
		if s != "runuser" {
			c.Fatalf(`expected to get "runuser", got %q`, s)
		}
		return "/sbin/runuser", nil
	})()

	c.Check(backend.PickUserWrapper(), check.Equals, "/sbin/runuser")
	c.Check(n, check.Equals, 1)
}

func (s *snapshotSuite) TestPickUserWrapperSudo(c *check.C) {
	n := 0
	defer backend.MockExecLookPath(func(s string) (string, error) {
		n++
		if n == 1 {
			if s != "runuser" {
				c.Fatalf(`expected to get "runuser" first, got %q`, s)
			}
			return "", errors.New("no such thing")
		}
		if s != "sudo" {
			c.Fatalf(`expected to get "sudo" next, got %q`, s)
		}
		return "/usr/bin/sudo", nil
	})()

	c.Check(backend.PickUserWrapper(), check.Equals, "/usr/bin/sudo")
	c.Check(n, check.Equals, 2)
}

func (s *snapshotSuite) TestPickUserWrapperNothing(c *check.C) {
	n := 0
	defer backend.MockExecLookPath(func(s string) (string, error) {
		n++
		return "", errors.New("no such thing")
	})()

	c.Check(backend.PickUserWrapper(), check.Equals, "")
	c.Check(n, check.Equals, 2)
}

func (s *snapshotSuite) TestMaybeRunuserHappyRunuser(c *check.C) {
	uid := sys.UserID(0)
	defer backend.MockSysGeteuid(func() sys.UserID { return uid })()
	defer backend.SetUserWrapper("/sbin/runuser")()
	logbuf, restore := logger.MockLogger()
	defer restore()

	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: "/sbin/runuser",
		Args: []string{"/sbin/runuser", "-u", "test", "--", "tar", "--bar"},
	})
	c.Check(backend.TarAsUser("root", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	uid = 42
	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(logbuf.String(), check.Equals, "")
}

func (s *snapshotSuite) TestMaybeRunuserHappySudo(c *check.C) {
	uid := sys.UserID(0)
	defer backend.MockSysGeteuid(func() sys.UserID { return uid })()
	defer backend.SetUserWrapper("/usr/bin/sudo")()
	logbuf, restore := logger.MockLogger()
	defer restore()

	cmd := backend.TarAsUser("test", "--bar")
	c.Check(cmd, check.DeepEquals, &exec.Cmd{
		Path: "/usr/bin/sudo",
		Args: []string{"/usr/bin/sudo", "-u", "test", "--", "tar", "--bar"},
	})
	c.Check(backend.TarAsUser("root", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	uid = 42
	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(logbuf.String(), check.Equals, "")
}

func (s *snapshotSuite) TestMaybeRunuserNoHappy(c *check.C) {
	uid := sys.UserID(0)
	defer backend.MockSysGeteuid(func() sys.UserID { return uid })()
	defer backend.SetUserWrapper("")()
	logbuf, restore := logger.MockLogger()
	defer restore()

	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(backend.TarAsUser("root", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	uid = 42
	c.Check(backend.TarAsUser("test", "--bar"), check.DeepEquals, &exec.Cmd{
		Path: s.tarPath,
		Args: []string{"tar", "--bar"},
	})
	c.Check(strings.TrimSpace(logbuf.String()), check.Matches, ".* No user wrapper found.*")
}

func (s *snapshotSuite) TestImport(c *check.C) {
	tempdir := c.MkDir()

	// create snapshot export file
	tarFile1 := path.Join(tempdir, "exported1.snapshot")
	err := createTestExportFile(tarFile1, &createTestExportFlags{exportJSON: true})
	c.Check(err, check.IsNil)

	// create an exported snapshot with missing export.json
	tarFile2 := path.Join(tempdir, "exported2.snapshot")
	err = createTestExportFile(tarFile2, &createTestExportFlags{})
	c.Check(err, check.IsNil)

	// create invalid exported file
	tarFile3 := path.Join(tempdir, "exported3.snapshot")
	err = ioutil.WriteFile(tarFile3, []byte("invalid"), 0755)
	c.Check(err, check.IsNil)

	// create an exported snapshot with a directory
	tarFile4 := path.Join(tempdir, "exported4.snapshot")
	flags := &createTestExportFlags{
		exportJSON: true,
		withDir:    true,
	}
	err = createTestExportFile(tarFile4, flags)
	c.Check(err, check.IsNil)

	type tableT struct {
		setID      uint64
		filename   string
		inProgress bool
		error      string
	}

	table := []tableT{
		{14, tarFile1, false, ""},
		{14, tarFile2, false, "cannot import snapshot 14: no export.json file in uploaded data"},
		{14, tarFile3, false, "cannot import snapshot 14: cannot read snapshot import: unexpected EOF"},
		{14, tarFile4, false, "cannot import snapshot 14: unexpected directory in import file"},
		{14, tarFile1, true, "cannot import snapshot 14: already in progress for this set id"},
	}

	for i, t := range table {
		comm := check.Commentf("%d: %d %s", i, t.setID, t.filename)

		// reset
		err = os.RemoveAll(dirs.SnapshotsDir)
		c.Assert(err, check.IsNil, comm)
		importingFile := filepath.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_importing", t.setID))
		if t.inProgress {
			err := os.MkdirAll(dirs.SnapshotsDir, 0700)
			c.Assert(err, check.IsNil, comm)
			err = ioutil.WriteFile(importingFile, nil, 0644)
			c.Assert(err, check.IsNil)
		} else {
			err = os.RemoveAll(importingFile)
			c.Assert(err, check.IsNil, comm)
		}

		f, err := os.Open(t.filename)
		c.Assert(err, check.IsNil, comm)
		defer f.Close()

		snapNames, err := backend.Import(context.Background(), t.setID, f, nil)
		if t.error != "" {
			c.Check(err, check.ErrorMatches, t.error, comm)
			continue
		}
		c.Check(err, check.IsNil, comm)
		sort.Strings(snapNames)
		c.Check(snapNames, check.DeepEquals, []string{"bar", "baz", "foo"})

		dir, err := os.Open(dirs.SnapshotsDir)
		c.Assert(err, check.IsNil, comm)
		defer dir.Close()
		names, err := dir.Readdirnames(100)
		c.Assert(err, check.IsNil, comm)
		c.Check(len(names), check.Equals, 3, comm)
	}
}

func (s *snapshotSuite) TestImportCheckError(c *check.C) {
	err := os.MkdirAll(dirs.SnapshotsDir, 0755)
	c.Assert(err, check.IsNil)

	// create snapshot export file
	tarFile1 := path.Join(c.MkDir(), "exported1.snapshot")
	flags := &createTestExportFlags{
		exportJSON:      true,
		corruptChecksum: true,
	}
	err = createTestExportFile(tarFile1, flags)
	c.Assert(err, check.IsNil)

	f, err := os.Open(tarFile1)
	c.Assert(err, check.IsNil)
	_, err = backend.Import(context.Background(), 14, f, nil)
	c.Assert(err, check.ErrorMatches, `cannot import snapshot 14: validation failed for .+/14_foo_1.0_199.zip": snapshot entry "archive.tgz" expected hash \(d5ef563…\) does not match actual \(6655519…\)`)
}

func (s *snapshotSuite) TestImportDuplicated(c *check.C) {
	err := os.MkdirAll(dirs.SnapshotsDir, 0755)
	c.Assert(err, check.IsNil)

	ctx := context.TODO()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	shID := uint64(12)

	shw, err := backend.Save(ctx, shID, info, nil, []string{"snapuser"}, nil)
	c.Assert(err, check.IsNil)

	export, err := backend.NewSnapshotExport(ctx, shw.SetID)
	c.Assert(err, check.IsNil)
	err = export.Init()
	c.Assert(err, check.IsNil)

	buf := bytes.NewBuffer(nil)
	c.Assert(export.StreamTo(buf), check.IsNil)
	c.Check(buf.Len(), check.Equals, int(export.Size()))

	// now import it
	_, err = backend.Import(ctx, 123, buf, nil)
	dupErr, ok := err.(backend.DuplicatedSnapshotImportError)
	c.Assert(ok, check.Equals, true)
	c.Assert(dupErr, check.DeepEquals, backend.DuplicatedSnapshotImportError{SetID: shID, SnapNames: []string{"hello-snap"}})
}

func (s *snapshotSuite) TestImportExportRoundtrip(c *check.C) {
	err := os.MkdirAll(dirs.SnapshotsDir, 0755)
	c.Assert(err, check.IsNil)

	ctx := context.TODO()

	epoch := snap.E("42*")
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}, Version: "v1.33", Epoch: epoch}
	cfg := map[string]interface{}{"some-setting": false}
	shID := uint64(12)

	shw, err := backend.Save(ctx, shID, info, cfg, []string{"snapuser"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)

	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	export, err := backend.NewSnapshotExport(ctx, shw.SetID)
	c.Assert(err, check.IsNil)
	err = export.Init()
	c.Assert(err, check.IsNil)

	buf := bytes.NewBuffer(nil)
	c.Assert(export.StreamTo(buf), check.IsNil)
	c.Check(buf.Len(), check.Equals, int(export.Size()))

	// now import it
	c.Assert(os.Remove(filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip")), check.IsNil)

	names, err := backend.Import(ctx, 123, buf, nil)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"hello-snap"})

	sets, err := backend.List(ctx, 0, nil)
	c.Assert(err, check.IsNil)
	c.Assert(sets, check.HasLen, 1)
	c.Check(sets[0].ID, check.Equals, uint64(123))

	rdr, err := backend.Open(filepath.Join(dirs.SnapshotsDir, "123_hello-snap_v1.33_42.zip"), backend.ExtractFnameSetID)
	defer rdr.Close()
	c.Check(err, check.IsNil)
	c.Check(rdr.SetID, check.Equals, uint64(123))
	c.Check(rdr.Snap, check.Equals, "hello-snap")
	c.Check(rdr.IsValid(), check.Equals, true)
}

func (s *snapshotSuite) TestEstimateSnapshotSize(c *check.C) {

	for _, t := range []struct {
		snapDir string
		opts    *dirs.SnapDirOptions
	}{
		{dirs.UserHomeSnapDir, nil},
		{dirs.UserHomeSnapDir, &dirs.SnapDirOptions{HiddenSnapDataDir: false}},
		{dirs.HiddenSnapDataHomeDir, &dirs.SnapDirOptions{HiddenSnapDataDir: true}}} {
		s.testEstimateSnapshotSize(c, t.snapDir, t.opts)
	}
}

func (s *snapshotSuite) testEstimateSnapshotSize(c *check.C, snapDataDir string, opts *dirs.SnapDirOptions) {
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{{HomeDir: filepath.Join(s.root, "home/user1")}}, nil
	})
	defer restore()

	var info = &snap.Info{
		SuggestedName: "foo",
		SideInfo: snap.SideInfo{
			Revision: snap.R(7),
		},
	}

	snapData := []string{
		"/var/snap/foo/7/somedatadir",
		"/var/snap/foo/7/otherdata",
		"/var/snap/foo/7",
		"/var/snap/foo/common",
		"/var/snap/foo/common/a",
		filepath.Join("/home/user1", snapDataDir, "foo/7/somedata"),
		filepath.Join("/home/user1", snapDataDir, "foo/common"),
	}
	var data []byte
	var expected int
	for _, d := range snapData {
		data = append(data, 0)
		expected += len(data)
		c.Assert(os.MkdirAll(filepath.Join(s.root, d), 0755), check.IsNil)
		c.Assert(ioutil.WriteFile(filepath.Join(s.root, d, "somefile"), data, 0644), check.IsNil)
	}

	sz, err := backend.EstimateSnapshotSize(info, nil, opts)
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(expected))
}

func (s *snapshotSuite) TestEstimateSnapshotSizeEmpty(c *check.C) {
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{{HomeDir: filepath.Join(s.root, "home/user1")}}, nil
	})
	defer restore()

	var info = &snap.Info{
		SuggestedName: "foo",
		SideInfo: snap.SideInfo{
			Revision: snap.R(7),
		},
	}

	snapData := []string{
		"/var/snap/foo/common",
		"/var/snap/foo/7",
		"/home/user1/snap/foo/7",
		"/home/user1/snap/foo/common",
	}
	for _, d := range snapData {
		c.Assert(os.MkdirAll(filepath.Join(s.root, d), 0755), check.IsNil)
	}

	sz, err := backend.EstimateSnapshotSize(info, nil, nil)
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(0))
}

func (s *snapshotSuite) TestEstimateSnapshotPassesUsernames(c *check.C) {
	var gotUsernames []string
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		gotUsernames = usernames
		return nil, nil
	})
	defer restore()

	var info = &snap.Info{
		SuggestedName: "foo",
		SideInfo: snap.SideInfo{
			Revision: snap.R(7),
		},
	}

	_, err := backend.EstimateSnapshotSize(info, []string{"user1", "user2"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(gotUsernames, check.DeepEquals, []string{"user1", "user2"})
}

func (s *snapshotSuite) TestEstimateSnapshotSizeNotDataDirs(c *check.C) {
	restore := backend.MockUsersForUsernames(func(usernames []string, _ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{{HomeDir: filepath.Join(s.root, "home/user1")}}, nil
	})
	defer restore()

	var info = &snap.Info{
		SuggestedName: "foo",
		SideInfo:      snap.SideInfo{Revision: snap.R(7)},
	}

	sz, err := backend.EstimateSnapshotSize(info, nil, nil)
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(0))
}

func (s *snapshotSuite) TestExportTwice(c *check.C) {
	// use mocking done in snapshotSuite.SetUpTest
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(42),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	// create a snapshot
	shID := uint64(12)
	_, err := backend.Save(context.TODO(), shID, info, nil, []string{"snapuser"}, nil)
	c.Check(err, check.IsNil)

	// content.json + num_files + export.json + footer
	expectedSize := int64(1024 + 4*512 + 1024 + 2*512)
	// do on export at the start of the epoch
	restore := backend.MockTimeNow(func() time.Time { return time.Time{} })
	defer restore()
	// export once
	buf := bytes.NewBuffer(nil)
	ctx := context.Background()
	se, err := backend.NewSnapshotExport(ctx, shID)
	c.Check(err, check.IsNil)
	err = se.Init()
	c.Assert(err, check.IsNil)
	c.Check(se.Size(), check.Equals, expectedSize)
	// and we can stream the data
	err = se.StreamTo(buf)
	c.Assert(err, check.IsNil)
	c.Check(buf.Len(), check.Equals, int(expectedSize))

	// and again to ensure size does not change when exported again
	//
	// Note that moving beyond year 2242 will change the tar format
	// used by the go internal tar and that will make the size actually
	// change.
	restore = backend.MockTimeNow(func() time.Time { return time.Date(2242, 1, 1, 12, 0, 0, 0, time.UTC) })
	defer restore()
	se2, err := backend.NewSnapshotExport(ctx, shID)
	c.Check(err, check.IsNil)
	err = se2.Init()
	c.Assert(err, check.IsNil)
	c.Check(se2.Size(), check.Equals, expectedSize)
	// and we can stream the data
	buf.Reset()
	err = se2.StreamTo(buf)
	c.Assert(err, check.IsNil)
	c.Check(buf.Len(), check.Equals, int(expectedSize))
}

func (s *snapshotSuite) TestExportUnhappy(c *check.C) {
	se, err := backend.NewSnapshotExport(context.Background(), 5)
	c.Assert(err, check.ErrorMatches, "no snapshot data found for 5")
	c.Assert(se, check.IsNil)
}

type createTestExportFlags struct {
	exportJSON      bool
	withDir         bool
	corruptChecksum bool
}

func createTestExportFile(filename string, flags *createTestExportFlags) error {
	tf, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer tf.Close()
	tw := tar.NewWriter(tf)
	defer tw.Close()

	for _, s := range []string{"foo", "bar", "baz"} {
		fname := fmt.Sprintf("5_%s_1.0_199.zip", s)

		buf := bytes.NewBuffer(nil)
		zipW := zip.NewWriter(buf)
		defer zipW.Close()

		sha := map[string]string{}

		// create dummy archive.tgz
		archiveWriter, err := zipW.CreateHeader(&zip.FileHeader{Name: "archive.tgz"})
		if err != nil {
			return err
		}
		var sz osutil.Sizer
		hasher := crypto.SHA3_384.New()
		out := io.MultiWriter(archiveWriter, hasher, &sz)
		if _, err := out.Write([]byte(s)); err != nil {
			return err
		}

		if flags.corruptChecksum {
			hasher.Write([]byte{0})
		}
		sha["archive.tgz"] = fmt.Sprintf("%x", hasher.Sum(nil))

		snapshot := backend.MockSnapshot(5, s, snap.Revision{N: 199}, sz.Size(), sha)

		// create meta.json
		metaWriter, err := zipW.Create("meta.json")
		if err != nil {
			return err
		}
		hasher = crypto.SHA3_384.New()
		enc := json.NewEncoder(io.MultiWriter(metaWriter, hasher))
		if err := enc.Encode(snapshot); err != nil {
			return err
		}

		// write meta.sha3_384
		metaSha3Writer, err := zipW.Create("meta.sha3_384")
		if err != nil {
			return err
		}
		fmt.Fprintf(metaSha3Writer, "%x\n", hasher.Sum(nil))
		zipW.Close()

		hdr := &tar.Header{
			Name: fname,
			Mode: 0644,
			Size: int64(buf.Len()),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(buf.Bytes()); err != nil {
			return err
		}
	}

	if flags.withDir {
		hdr := &tar.Header{
			Name:     dirs.SnapshotsDir,
			Mode:     0700,
			Size:     int64(0),
			Typeflag: tar.TypeDir,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err = tw.Write([]byte("")); err != nil {
			return nil
		}
	}

	if flags.exportJSON {
		exp := fmt.Sprintf(`{"format":1, "date":"%s"}`, time.Now().Format(time.RFC3339))
		hdr := &tar.Header{
			Name: "export.json",
			Mode: 0644,
			Size: int64(len(exp)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err = tw.Write([]byte(exp)); err != nil {
			return nil
		}
	}

	return nil
}

func makeMockSnapshotZipContent(c *check.C) []byte {
	buf := bytes.NewBuffer(nil)
	zipW := zip.NewWriter(buf)

	// create dummy archive.tgz
	archiveWriter, err := zipW.CreateHeader(&zip.FileHeader{Name: "archive.tgz"})
	c.Assert(err, check.IsNil)
	_, err = archiveWriter.Write([]byte("mock archive.tgz content"))
	c.Assert(err, check.IsNil)

	// create dummy meta.json
	archiveWriter, err = zipW.CreateHeader(&zip.FileHeader{Name: "meta.json"})
	c.Assert(err, check.IsNil)
	_, err = archiveWriter.Write([]byte("{}"))
	c.Assert(err, check.IsNil)

	zipW.Close()
	return buf.Bytes()
}

func (s *snapshotSuite) TestIterWithMockedSnapshotFiles(c *check.C) {
	err := os.MkdirAll(dirs.SnapshotsDir, 0755)
	c.Assert(err, check.IsNil)

	fn := "1_hello_1.0_x1.zip"
	err = ioutil.WriteFile(filepath.Join(dirs.SnapshotsDir, fn), makeMockSnapshotZipContent(c), 0644)
	c.Assert(err, check.IsNil)

	callbackCalled := 0
	f := func(snapshot *backend.Reader) error {
		callbackCalled++
		return nil
	}

	err = backend.Iter(context.Background(), f)
	c.Check(err, check.IsNil)
	c.Check(callbackCalled, check.Equals, 1)

	// now pretend we are importing snapshot id 1
	callbackCalled = 0
	fn = "1_importing"
	err = ioutil.WriteFile(filepath.Join(dirs.SnapshotsDir, fn), nil, 0644)
	c.Assert(err, check.IsNil)

	// and while importing Iter() does not call the callback
	err = backend.Iter(context.Background(), f)
	c.Check(err, check.IsNil)
	c.Check(callbackCalled, check.Equals, 0)
}

func (s *snapshotSuite) TestCleanupAbandondedImports(c *check.C) {
	err := os.MkdirAll(dirs.SnapshotsDir, 0755)
	c.Assert(err, check.IsNil)

	// create 2 snapshot IDs 1,2
	snapshotFiles := map[int][]string{}
	for i := 1; i < 3; i++ {
		fn := fmt.Sprintf("%d_hello_%d.0_x1.zip", i, i)
		p := filepath.Join(dirs.SnapshotsDir, fn)
		snapshotFiles[i] = append(snapshotFiles[i], p)
		err = ioutil.WriteFile(p, makeMockSnapshotZipContent(c), 0644)
		c.Assert(err, check.IsNil)

		fn = fmt.Sprintf("%d_olleh_%d.0_x1.zip", i, i)
		p = filepath.Join(dirs.SnapshotsDir, fn)
		snapshotFiles[i] = append(snapshotFiles[i], p)
		err = ioutil.WriteFile(p, makeMockSnapshotZipContent(c), 0644)
		c.Assert(err, check.IsNil)
	}

	// pretend setID 2 has a import file which means which means that
	// an import was started in the past but did not complete
	fn := "2_importing"
	err = ioutil.WriteFile(filepath.Join(dirs.SnapshotsDir, fn), nil, 0644)
	c.Assert(err, check.IsNil)

	// run cleanup
	cleaned, err := backend.CleanupAbandondedImports()
	c.Check(cleaned, check.Equals, 1)
	c.Check(err, check.IsNil)

	// id1 untouched
	c.Check(snapshotFiles[1][0], testutil.FilePresent)
	c.Check(snapshotFiles[1][1], testutil.FilePresent)
	// id2 cleaned
	c.Check(snapshotFiles[2][0], testutil.FileAbsent)
	c.Check(snapshotFiles[2][1], testutil.FileAbsent)
}

func (s *snapshotSuite) TestCleanupAbandondedImportsFailMany(c *check.C) {
	restore := backend.MockFilepathGlob(func(string) ([]string, error) {
		return []string{
			"/var/lib/snapd/snapshots/NaN_importing",
			"/var/lib/snapd/snapshots/11_importing",
			"/var/lib/snapd/snapshots/22_importing",
		}, nil
	})
	defer restore()

	_, err := backend.CleanupAbandondedImports()
	c.Assert(err, check.ErrorMatches, `cannot cleanup imports:
- cannot determine snapshot id from "/var/lib/snapd/snapshots/NaN_importing"
- cannot cancel import for set id 11:
 - remove /.*/var/lib/snapd/snapshots/11_importing: no such file or directory
- cannot cancel import for set id 22:
 - remove /.*/var/lib/snapd/snapshots/22_importing: no such file or directory`)
}

func (s *snapshotSuite) TestMultiError(c *check.C) {
	me2 := backend.NewMultiError("deeper nested wrongness", []error{
		fmt.Errorf("some error in level 2"),
	})
	me1 := backend.NewMultiError("nested wrongness", []error{
		fmt.Errorf("some error in level 1"),
		me2,
		fmt.Errorf("other error in level 1"),
	})
	me := backend.NewMultiError("many things went wrong", []error{
		fmt.Errorf("some normal error"),
		me1,
	})

	c.Check(me, check.ErrorMatches, `many things went wrong:
- some normal error
- nested wrongness:
 - some error in level 1
 - deeper nested wrongness:
  - some error in level 2
 - other error in level 1`)

	// do it again
	c.Check(me, check.ErrorMatches, `many things went wrong:
- some normal error
- nested wrongness:
 - some error in level 1
 - deeper nested wrongness:
  - some error in level 2
 - other error in level 1`)
}

func (s *snapshotSuite) TestMultiErrorCycle(c *check.C) {
	errs := []error{nil, fmt.Errorf("e5")}
	me5 := backend.NewMultiError("he5", errs)
	// very hard to happen in practice
	errs[0] = me5
	me4 := backend.NewMultiError("he4", []error{me5})
	me3 := backend.NewMultiError("he3", []error{me4})
	me2 := backend.NewMultiError("he3", []error{me3})
	me1 := backend.NewMultiError("he1", []error{me2})
	me := backend.NewMultiError("he", []error{me1})

	c.Check(me, check.ErrorMatches, `he:
- he1:
 - he3:
  - he3:
   - he4:
    - he5:
     - he5:
      - he5:
       - he5:
        - circular or too deep error nesting \(max 8\)\?!
        - e5
       - e5
      - e5
     - e5`)
}

func (s *snapshotSuite) TestSnapshotExportContentHash(c *check.C) {
	ctx := context.TODO()
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(42),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	shID := uint64(12)
	shw, err := backend.Save(ctx, shID, info, nil, []string{"snapuser"}, nil)
	c.Check(err, check.IsNil)

	// now export it
	export, err := backend.NewSnapshotExport(ctx, shw.SetID)
	c.Assert(err, check.IsNil)
	c.Check(export.ContentHash(), check.HasLen, sha256.Size)

	// and check that exporting it again leads to the same content hash
	export2, err := backend.NewSnapshotExport(ctx, shw.SetID)
	c.Assert(err, check.IsNil)
	c.Check(export.ContentHash(), check.DeepEquals, export2.ContentHash())

	// but changing the snapshot changes the content hash
	info = &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(9999),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	shw, err = backend.Save(ctx, shID, info, nil, []string{"snapuser"}, nil)
	c.Check(err, check.IsNil)

	export3, err := backend.NewSnapshotExport(ctx, shw.SetID)
	c.Assert(err, check.IsNil)
	c.Check(export.ContentHash(), check.Not(check.DeepEquals), export3.ContentHash())
}
