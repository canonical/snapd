// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/progress"

	. "gopkg.in/check.v1"
)

type remoteRepoTestSuite struct {
	store *SnapUbuntuStoreRepository

	origDownloadFunc func(string, io.Writer, *http.Request, progress.Meter) error
}

var _ = Suite(&remoteRepoTestSuite{})

func (t *remoteRepoTestSuite) SetUpTest(c *C) {
	t.store = NewUbuntuStoreSnapRepository()
	t.origDownloadFunc = download
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), IsNil)
}

func (t *remoteRepoTestSuite) TearDownTest(c *C) {
	download = t.origDownloadFunc
}

func (t *remoteRepoTestSuite) TestDownloadOK(c *C) {
	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		w.Write([]byte("I was downloaded"))
		return nil
	}

	path, err := t.store.Download(&RemoteSnapPart{}, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I was downloaded")
}

func (t *remoteRepoTestSuite) TestDownloadFails(c *C) {
	var tmpfile *os.File
	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		return fmt.Errorf("uh, it failed")
	}

	// simulate a failed download
	path, err := t.store.Download(&RemoteSnapPart{}, nil)
	c.Assert(err, ErrorMatches, "uh, it failed")
	c.Assert(path, Equals, "")
	// ... and ensure that the tempfile is removed
	c.Assert(helpers.FileExists(tmpfile.Name()), Equals, false)
}

func (t *remoteRepoTestSuite) TestDownloadSyncFails(c *C) {
	var tmpfile *os.File
	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		w.Write([]byte("sync will fail"))
		err := tmpfile.Close()
		c.Assert(err, IsNil)
		return nil
	}

	// simulate a failed sync
	path, err := t.store.Download(&RemoteSnapPart{}, nil)
	c.Assert(err, ErrorMatches, "fsync:.*")
	c.Assert(path, Equals, "")
	// ... and ensure that the tempfile is removed
	c.Assert(helpers.FileExists(tmpfile.Name()), Equals, false)
}
