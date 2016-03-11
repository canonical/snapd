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

package state_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
)

type fileBackendSuite struct{}

var _ = Suite(&fileBackendSuite{})

func (fsbss *fileBackendSuite) TestOpen(c *C) {
	sf := state.NewFileBackend("test.state")
	c.Assert(sf, FitsTypeOf, state.FileBackend)
}

func (fsbss *fileBackendSuite) TestCheckpoint(c *C) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	backFn := filepath.Join(c.MkDir(), "test.state")
	sf := state.NewFileBackend(backFn)

	canary := []byte("some-data")
	err := sf.Checkpoint(canary)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(backFn)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canary)

	st, err := os.Stat(backFn)
	c.Assert(err, IsNil)
	c.Assert(st.Mode(), Equals, os.FileMode(0600))
}
