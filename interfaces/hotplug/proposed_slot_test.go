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

package hotplug

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type proposedSlotSuite struct {
	testutil.BaseTest
}

var _ = Suite(&proposedSlotSuite{})

func (s *proposedSlotSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
}

func (s *proposedSlotSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func (s *proposedSlotSuite) TestCleanHappy(c *C) {
	slot := &ProposedSlot{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}
	slot := mylog.Check2(slot.Clean())

	c.Assert(slot, DeepEquals, &ProposedSlot{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
}

func (s *proposedSlotSuite) TestNilAttrs(c *C) {
	slot := &ProposedSlot{Name: "slot"}
	slot := mylog.Check2(slot.Clean())

	c.Assert(slot, DeepEquals, &ProposedSlot{Name: "slot", Attrs: map[string]interface{}{}})
}

func (s *proposedSlotSuite) TestDeepCopy(c *C) {
	attrs := map[string]interface{}{"foo": "bar"}
	slot := &ProposedSlot{Name: "slot1", Attrs: attrs}
	slot := mylog.Check2(slot.Clean())

	attrs["foo"] = "modified"
	c.Assert(slot, DeepEquals, &ProposedSlot{Name: "slot1", Label: "", Attrs: map[string]interface{}{"foo": "bar"}})
}

func (s *proposedSlotSuite) TestEmptyName(c *C) {
	slot := &ProposedSlot{Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}
	slot := mylog.Check2(slot.Clean())

	c.Assert(slot, DeepEquals, &ProposedSlot{Name: "", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
}

func (s *proposedSlotSuite) TestInvalidName(c *C) {
	slot := &ProposedSlot{Name: "slot!"}
	slot := mylog.Check2(slot.Clean())
	c.Assert(err, ErrorMatches, `invalid slot name: "slot!"`)
	c.Assert(slot, IsNil)
}
