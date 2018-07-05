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

func (s *hotplugSpecSuite) TestAddSlot(c *C) {
	spec := NewSpecification()
	c.Assert(spec.AddSlot(&SlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}), IsNil)
	c.Assert(spec.AddSlot(&SlotSpec{Name: "slot2", Label: "A slot", Attrs: map[string]interface{}{"baz": "booze"}}), IsNil)

	c.Assert(spec.Slots(), DeepEquals, []*SlotSpec{
		{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}},
		{Name: "slot2", Label: "A slot", Attrs: map[string]interface{}{"baz": "booze"}},
	})

	c.Assert(spec.AddSlot(&SlotSpec{Name: "---", Label: "A slot", Attrs: map[string]interface{}{}}), ErrorMatches, `invalid slot name: "---"`)
}

func (s *hotplugSpecSuite) TestAddSlotDuplicate(c *C) {
	spec := NewSpecification()
	c.Assert(spec.AddSlot(&SlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}}), IsNil)
	err := spec.AddSlot(&SlotSpec{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}})
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `slot "slot1" already exists`)

	c.Assert(spec.Slots(), DeepEquals, []*SlotSpec{
		{Name: "slot1", Label: "A slot", Attrs: map[string]interface{}{"foo": "bar"}},
	})
}
