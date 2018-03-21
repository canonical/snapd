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
	"encoding/json"
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

func (s *snapshotSuite) TestHappyRoundtrip(c *check.C) {
	logger.SimpleSetup()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42)}, Version: "v1.33"}
	cfg := json.RawMessage(`{"some-setting":false}`)
	shID := uint64(12)

	shw, err := backend.Save(context.TODO(), shID, info, &cfg, []string{"snapuser"})
	c.Assert(err, check.IsNil)
	c.Check(shw.SetID, check.Equals, shID)
	c.Check(shw.Snap, check.Equals, info.Name())
	c.Check(shw.Version, check.Equals, info.Version)
	c.Check(shw.Revision, check.Equals, info.Revision)
	c.Check(string(*shw.Config), check.DeepEquals, string(cfg))
	c.Check(backend.Filename(shw), check.Equals, filepath.Join(dirs.SnapshotDir, "hello-snap_v1.33_12.zip"))
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
	c.Check(string(*shr.Config), check.DeepEquals, string(cfg))
	c.Check(shr.Filename(), check.Equals, backend.Filename(shw))
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
