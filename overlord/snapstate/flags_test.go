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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

type flagsSuite struct{}

var _ = Suite(&flagsSuite{})

func (s *flagsSuite) TestEffectiveConfinement(c *C) {
	f := snapstate.Flags{}
	// In absence of jailmode or devmode flags, confinement is unchanged
	c.Assert(f.EffectiveConfinement(snap.ClassicConfinement), Equals, snap.ClassicConfinement)
	c.Assert(f.EffectiveConfinement(snap.DevmodeConfinement), Equals, snap.DevmodeConfinement)
	c.Assert(f.EffectiveConfinement(snap.StrictConfinement), Equals, snap.StrictConfinement)
	// When devmode flag is set it can override strict confinement
	f = snapstate.Flags{DevMode: true}
	c.Assert(f.EffectiveConfinement(snap.ClassicConfinement), Equals, snap.ClassicConfinement)
	c.Assert(f.EffectiveConfinement(snap.DevmodeConfinement), Equals, snap.DevmodeConfinement)
	c.Assert(f.EffectiveConfinement(snap.StrictConfinement), Equals, snap.DevmodeConfinement)
	// When jailmode flag is set it can override devmode confinement
	f = snapstate.Flags{JailMode: true}
	c.Assert(f.EffectiveConfinement(snap.ClassicConfinement), Equals, snap.ClassicConfinement)
	c.Assert(f.EffectiveConfinement(snap.DevmodeConfinement), Equals, snap.StrictConfinement)
	c.Assert(f.EffectiveConfinement(snap.StrictConfinement), Equals, snap.StrictConfinement)
	// When both devmode and jailmode flags are set then jailmode is the stronger one
	f = snapstate.Flags{DevMode: true, JailMode: true}
	c.Assert(f.EffectiveConfinement(snap.ClassicConfinement), Equals, snap.ClassicConfinement)
	c.Assert(f.EffectiveConfinement(snap.DevmodeConfinement), Equals, snap.StrictConfinement)
	c.Assert(f.EffectiveConfinement(snap.StrictConfinement), Equals, snap.StrictConfinement)
}
