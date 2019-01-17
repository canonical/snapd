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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type hotplugSpecSuite struct {
	testutil.BaseTest
}

var _ = Suite(&hotplugSpecSuite{})

func (s *hotplugSpecSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
}

func (s *hotplugSpecSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func (s *hotplugSpecSuite) TestSetSlot(c *C) {
	spec := NewSpecification()
	c.Assert(spec.SetSlot(&RequestedSlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}), IsNil)
	c.Assert(spec.Slot(), DeepEquals, &RequestedSlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
}

func (s *hotplugSpecSuite) TestSetSlotEmptyName(c *C) {
	spec := NewSpecification()
	c.Assert(spec.SetSlot(&RequestedSlotSpec{Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}), IsNil)
	c.Assert(spec.Slot(), DeepEquals, &RequestedSlotSpec{Name: "", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
}

func (s *hotplugSpecSuite) TestSetSlotInvalidName(c *C) {
	spec := NewSpecification()
	err := spec.SetSlot(&RequestedSlotSpec{Name: "slot!", Label: "A slot"})
	c.Assert(err, ErrorMatches, `invalid slot name: "slot!"`)
}

func (s *hotplugSpecSuite) TestAddSlotAlreadyCreated(c *C) {
	spec := NewSpecification()
	c.Assert(spec.SetSlot(&RequestedSlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}), IsNil)
	err := spec.SetSlot(&RequestedSlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `slot specification already created`)

	c.Assert(spec.Slot(), DeepEquals, &RequestedSlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
}
