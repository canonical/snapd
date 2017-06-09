// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package interfaces_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
)

type systemKeySuite struct{}

var _ = Suite(&systemKeySuite{})

func (ts *systemKeySuite) TestInterfaceDigest(c *C) {
	restore := interfaces.MockSystemKeyInputs([]string{"build-id: some-build-id"})
	defer restore()

	systemKey := interfaces.SystemKey()
	c.Check(systemKey, Equals, "cbf4ec4c0ce8bf8c971284803a1cd863")

	// check that changing the inputs changes the output
	restore = interfaces.MockSystemKeyInputs([]string{"build-id: some-build-id", "kernel-apparmor: dbus,file,namespaces"})
	defer restore()
	c.Check(interfaces.SystemKey(), Not(Equals), systemKey)
}
