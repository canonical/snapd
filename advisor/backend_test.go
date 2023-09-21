// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package advisor_test

import (
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/dirs"
)

type backendSuite struct{}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapCacheDir, 0755), IsNil)

	// create an empty DB
	db, err := advisor.Create()
	c.Assert(err, IsNil)
	err = db.Commit()
	c.Assert(err, IsNil)
}

func dumpCommands(c *C) map[string]string {
	cmds, err := advisor.DumpCommands()
	c.Assert(err, IsNil)
	return cmds
}

func (s *backendSuite) TestCreateCommit(c *C) {
	expectedCommands := map[string]string{
		"meh": `[{"snap":"foo","version":"1.0"}]`,
		"foo": `[{"snap":"foo","version":"1.0"}]`,
	}

	db, err := advisor.Create()
	c.Assert(err, IsNil)
	c.Assert(db.AddSnap("foo", "1.0", "foo summary", []string{"foo", "meh"}), IsNil)
	// adding does not change the DB
	c.Check(dumpCommands(c), DeepEquals, map[string]string{})
	// but commit does
	c.Assert(db.Commit(), IsNil)
	c.Check(dumpCommands(c), DeepEquals, expectedCommands)
}

func (s *backendSuite) TestCreateRollback(c *C) {
	db, err := advisor.Create()
	c.Assert(err, IsNil)
	// adding does not change the DB
	c.Assert(db.AddSnap("foo", "1.0", "foo summary", []string{"foo", "meh"}), IsNil)
	// and rollback ensures any change is reverted
	c.Assert(db.Rollback(), IsNil)
	c.Check(dumpCommands(c), DeepEquals, map[string]string{})
}
