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
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
)

type StateFsBackendSuite struct{}

var _ = Suite(&StateFsBackendSuite{})

func (fsbss *StateFsBackendSuite) TestOpen(c *C) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	sf, err := state.OpenStateFsBackend(filepath.Join(c.MkDir(), "test.state"))
	c.Assert(err, IsNil)
	c.Assert(sf, FitsTypeOf, &state.StateFsBackend{})
}

func (fsbss *StateFsBackendSuite) TestCheckpoint(c *C) {
	backFn := filepath.Join(c.MkDir(), "test.state")
	sf, err := state.OpenStateFsBackend(backFn)
	c.Assert(err, IsNil)

	canary := []byte("some-data")
	err = sf.Checkpoint(canary)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(backFn)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canary)
}
