// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package mount_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/mount"
)

type recorderSuite struct{}

var _ = Suite(&recorderSuite{})

func (s *recorderSuite) TestSmoke(c *C) {
	ent0 := mount.Entry{FsName: "fs1"}
	ent1 := mount.Entry{FsName: "fs2"}
	rec := mount.Recorder{}
	rec.AddMountEntry(ent0)
	rec.AddMountEntry(ent1)
	c.Assert(rec.MountEntries, DeepEquals, []mount.Entry{ent0, ent1})
}
