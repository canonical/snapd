// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"testing"

	"golang.org/x/net/context"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/snap"
)

type snapshotSuite struct {
	root    string
	restore []func()
}

var _ = check.Suite(&snapshotSuite{})

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
			dir:     si.UserDataDir(homeDir),
			name:    "ufoo",
			content: "versioned user canary\n",
		}, {
			dir:     si.UserCommonDataDir(homeDir),
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
	}))
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
	defer backend.MockOpen(func(string) (*backend.Reader, error) {
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
		return []string{"hello"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string) (*backend.Reader, error) {
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
	c.Check(logbuf.String(), check.Matches, `(?m).* Cannot open snapshot "hello": invalid argument.`)
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
		return []string{"hello"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string) (*backend.Reader, error) {
		triedToOpenSnapshot = true
		// NOTE non-nil reader, and error, returned
		r := backend.Reader{}
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
		return []string{"hello"}, nil
	})()
	triedToOpenSnapshot := false
	defer backend.MockOpen(func(string) (*backend.Reader, error) {
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
			fmt.Sprintf("%d_foo", readNames),
			fmt.Sprintf("%d_bar", readNames),
			fmt.Sprintf("%d_baz", readNames),
		}, nil
	})()
	defer backend.MockOpen(func(fn string) (*backend.Reader, error) {
		var id uint64
		var snapname string
		fn = filepath.Base(fn)
		_, err := fmt.Sscanf(fn, "%d_%s", &id, &snapname)
		c.Assert(err, check.IsNil, check.Commentf(fn))
		f, err := os.Open(os.DevNull)
		c.Assert(err, check.IsNil, check.Commentf(fn))
		return &backend.Reader{
			File: f,
			Snapshot: client.Snapshot{
				SetID:    id,
				Snap:     snapname,
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
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	logger.SimpleSetup()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42)}, Version: "v1.33"}
	cfg := map[string]interface{}{"some-setting": false}
	shID := uint64(12)

	shw, err := backend.Save(context.TODO(), shID, info, cfg, []string{"snapuser"})
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)
	c.Check(shw.Snap, check.Equals, info.InstanceName())
	c.Check(shw.Version, check.Equals, info.Version)
	c.Check(shw.Revision, check.Equals, info.Revision)
	c.Check(shw.Conf, check.DeepEquals, cfg)
	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	shs, err := backend.List(context.TODO(), 0, nil)
	c.Assert(err, check.IsNil)
	c.Assert(shs, check.HasLen, 1)

	shr, err := backend.Open(backend.Filename(shw))
	c.Assert(err, check.IsNil)
	defer shr.Close()

	c.Check(shr.SetID, check.Equals, shID)
	c.Check(shr.Snap, check.Equals, info.InstanceName())
	c.Check(shr.Version, check.Equals, info.Version)
	c.Check(shr.Revision, check.Equals, info.Revision)
	c.Check(shr.Conf, check.DeepEquals, cfg)
	c.Check(shr.Name(), check.Equals, filepath.Join(dirs.SnapshotsDir, "12_hello-snap_v1.33_42.zip"))
	c.Check(shr.SHA3_384, check.DeepEquals, shw.SHA3_384)

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
		rs, err := shr.Restore(context.TODO(), nil, logger.Debugf)
		c.Assert(err, check.IsNil, comm)
		rs.Cleanup()
		c.Check(diff().Run(), check.IsNil, comm)

		// dirty it -> no longer like it was
		c.Check(ioutil.WriteFile(filepath.Join(info.DataDir(), "marker"), []byte("scribble\n"), 0644), check.IsNil, comm)
	}
}
