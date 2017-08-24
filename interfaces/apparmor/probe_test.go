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

package apparmor_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/apparmor"
)

type probeSuite struct{}

var _ = Suite(&probeSuite{})

func (s *probeSuite) TestProbe(c *C) {
	for _, l := range []apparmor.FeatureLevel{apparmor.None, apparmor.Partial, apparmor.Full} {
		restore := apparmor.MockFeatureLevel(l)
		defer restore()
		c.Assert(apparmor.Probe(), Equals, l, Commentf("was hoping for %q", l))
	}
}
