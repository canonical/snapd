// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/fdestate"
)

func (s *fdeMgrSuite) TestKeyslotsNotFoundErrorMessage(c *C) {
	err := fdestate.KeyslotRefsNotFoundError{KeyslotRefs: []fdestate.KeyslotRef{{ContainerRole: "some-role", Name: "some-slot"}}}
	c.Check(err.Error(), Equals, `key slot reference (container-role: "some-role", name: "some-slot") not found`)

	err = fdestate.KeyslotRefsNotFoundError{KeyslotRefs: []fdestate.KeyslotRef{
		{ContainerRole: "some-role", Name: "some-slot-1"},
		{ContainerRole: "some-role", Name: "some-slot-2"},
	}}
	c.Check(err.Error(), Equals, `key slot references [(container-role: "some-role", name: "some-slot-1"), (container-role: "some-role", name: "some-slot-2")] not found`)
}

func (s *fdeMgrSuite) TestKeyslotsAlreadyExistsError(c *C) {
	err := fdestate.KeyslotsAlreadyExistsError([]fdestate.Keyslot{{ContainerRole: "some-role", Name: "some-slot"}})
	c.Check(err.Error(), Equals, `key slot (container-role: "some-role", name: "some-slot") already exists`)

	err = fdestate.KeyslotsAlreadyExistsError([]fdestate.Keyslot{
		{ContainerRole: "some-role", Name: "some-slot-1"},
		{ContainerRole: "some-role", Name: "some-slot-2"},
	})
	c.Check(err.Error(), Equals, `key slots [(container-role: "some-role", name: "some-slot-1"), (container-role: "some-role", name: "some-slot-2")] already exist`)
}
