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

func hashkeys(sh *client.Snapshot) (keys []string) {
	for k := range sh.SHA3_384 {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

func (s *snapshotSuite) TestIterBailsIfContextDone(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	triedToOpenDir := false
	defer backend.MockDirOpen(func(string) (*os.File, error) {
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
	defer backend.MockDirOpen(func(string) (*os.File, error) {
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

func (s *snapshotSuite) TestIterReturnsOkIfSnapshotDirNonexistent(c *check.C) {
	triedToOpenDir := false
	defer backend.MockDirOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return nil, os.ErrNotExist
	})()

	err := backend.Iter(context.Background(), nil)
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
}

func (s *snapshotSuite) TestIterBailsIfSnapshotDirFails(c *check.C) {
	triedToOpenDir := false
	defer backend.MockDirOpen(func(string) (*os.File, error) {
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
	defer backend.MockDirOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return os.Open(os.DevNull)
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
	f := func(sh *backend.Reader) error {
		calledF = true
		return nil
	}

	err := backend.Iter(context.Background(), f)
	// snapshot open errors are not failures:
	c.Check(err, check.IsNil)
	c.Check(triedToOpenDir, check.Equals, true)
	c.Check(readNames, check.Equals, 2)
	c.Check(triedToOpenSnapshot, check.Equals, true)
	c.Check(logbuf.String(), check.Matches, `.* cannot open snapshot "hello": invalid argument\n`)
	c.Check(calledF, check.Equals, false)
}

func (s *snapshotSuite) TestIterCallsFuncIfSnapshotNotNil(c *check.C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	triedToOpenDir := false
	defer backend.MockDirOpen(func(string) (*os.File, error) {
		triedToOpenDir = true
		return os.Open(os.DevNull)
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
	f := func(sh *backend.Reader) error {
		c.Check(sh.Broken, check.Equals, "xyzzy")
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

func (s *snapshotSuite) xTestHappyRoundtrip(c *check.C) {
	logger.SimpleSetup()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42)}, Version: "v1.33"}
	cfg := map[string]interface{}{"some-setting": false}
	shID := uint64(12)

	shw, err := backend.Save(context.TODO(), shID, info, cfg, []string{"snapuser"})
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)
	c.Check(shw.Snap, check.Equals, info.Name())
	c.Check(shw.Version, check.Equals, info.Version)
	c.Check(shw.Revision, check.Equals, info.Revision)
	c.Check(shw.Conf, check.DeepEquals, cfg)
	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotDir, "12_hello-snap_v1.33.zip"))
	c.Check(hashkeys(shw), check.DeepEquals, []string{"archive.tgz", "user/snapuser.tgz"})

	shs, err := backend.List(context.TODO(), 0, nil)
	c.Assert(err, check.IsNil)
	c.Assert(shs, check.HasLen, 1)

	shr, err := backend.Open(backend.Filename(shw))
	c.Assert(err, check.IsNil)
	defer shr.Close()

	c.Check(shr.SetID, check.Equals, shID)
	c.Check(shr.Snap, check.Equals, info.Name())
	c.Check(shr.Version, check.Equals, info.Version)
	c.Check(shr.Revision, check.Equals, info.Revision)
	c.Check(shr.Conf, check.DeepEquals, cfg)
	c.Check(shr.Name(), check.Equals, filepath.Join(dirs.SnapshotDir, "12_hello-snap_v1.33.zip"))
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
		c.Assert(shr.Restore(context.TODO(), nil, logger.Debugf), check.IsNil, comm)
		c.Check(diff().Run(), check.IsNil, comm)

		// dirty it -> no longer like it was
		c.Check(ioutil.WriteFile(filepath.Join(info.DataDir(), "marker"), []byte("scribble\n"), 0644), check.IsNil, comm)
	}
}
