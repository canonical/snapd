// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C)2019 Canonical Ltd
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

package snap_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type hotplugKeySuite struct{}

var _ = Suite(&hotplugKeySuite{})

func (*hotplugKeySuite) TestShortString(c *C) {
	var key snap.HotplugKey
	key = "abcdefghijklmnopqrstuvwxyz"
	c.Check(key.ShortString(), Equals, "abcdefghijkl…")
}

func (*hotplugKeySuite) TestString(c *C) {
	// simple validity test
	keyStr := "abcdefghijklmnopqrstuvwxyz"
	key := snap.HotplugKey(keyStr)
	c.Check(fmt.Sprintf("%s", key), Equals, "abcdefghijklmnopqrstuvwxyz")
}
