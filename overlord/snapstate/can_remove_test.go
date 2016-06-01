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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"

	"github.com/snapcore/snapd/overlord/snapstate"
)

type canRemoveSuite struct {
	onClassic bool
}

var _ = Suite(&canRemoveSuite{})

func (s *canRemoveSuite) SetUpTest(c *C) {
	s.onClassic = release.OnClassic
}

func (s *canRemoveSuite) TearDownTest(c *C) {
	release.OnClassic = s.onClassic
}

func (s *canRemoveSuite) TestAppAreAlwaysOKToRemove(c *C) {
	info := &snap.Info{
		Type: snap.TypeApp,
	}
	info.OfficialName = "foo"

	c.Check(snapstate.CanRemove(info, false), Equals, true)
	c.Check(snapstate.CanRemove(info, true), Equals, true)
}

func (s *canRemoveSuite) TestActiveGadgetsAreNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.OfficialName = "foo"

	c.Check(snapstate.CanRemove(info, false), Equals, true)
	c.Check(snapstate.CanRemove(info, true), Equals, false)
}

func (s *canRemoveSuite) TestActiveOSAndKernelAreNotOK(c *C) {
	os := &snap.Info{
		Type: snap.TypeOS,
	}
	os.OfficialName = "os"
	kernel := &snap.Info{
		Type: snap.TypeKernel,
	}
	kernel.OfficialName = "krnl"

	c.Check(snapstate.CanRemove(os, false), Equals, true)
	c.Check(snapstate.CanRemove(os, true), Equals, false)

	c.Check(snapstate.CanRemove(kernel, false), Equals, true)
	c.Check(snapstate.CanRemove(kernel, true), Equals, false)
}
