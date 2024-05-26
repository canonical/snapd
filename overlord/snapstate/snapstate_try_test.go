// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package snapstate_test

import (
	"bytes"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/snapstate"
)

func (s *snapmgrTestSuite) TestTrySetsTryMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTrySetsTryModeDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}

func (s *snapmgrTestSuite) TestTrySetsTryModeJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}

func (s *snapmgrTestSuite) TestTrySetsTryModeClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c, "confinement: classic\n")
}

func (s *snapmgrTestSuite) testTrySetsTryMode(flags snapstate.Flags, c *C, extraYaml ...string) {
	s.state.Lock()
	defer s.state.Unlock()

	// make mock try dir
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	tryYaml := filepath.Join(d, "meta", "snap.yaml")
	mylog.Check(os.MkdirAll(filepath.Dir(tryYaml), 0755))

	buf := bytes.Buffer{}
	buf.WriteString("name: foo\nversion: 1.0\n")
	if len(extraYaml) > 0 {
		for _, extra := range extraYaml {
			buf.WriteString(extra)
		}
	}
	mylog.Check(os.WriteFile(tryYaml, buf.Bytes(), 0644))


	chg := s.state.NewChange("try", "try snap")
	ts := mylog.Check2(snapstate.TryPath(s.state, "foo", d, flags))

	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is in TryMode
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "foo", &snapst))


	flags.TryMode = true
	c.Check(snapst.Flags, DeepEquals, flags)

	c.Check(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Check(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prerequisites",
		"prepare-snap",
		"mount-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook[install]",
		"run-hook[default-configure]",
		"start-snap-services",
		"run-hook[configure]",
		"run-hook[check-health]",
	})
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlag(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()
	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c, "confinement: classic\n")
}
