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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snappy"
)

type flagscompatSuite struct{}

var _ = Suite(&flagscompatSuite{})

const (
	// copy here of the legacy values for when we drop snappy

	snappyAllowUnauthenticated = 1 << iota
	snappyInhibitHooks
	snappyDoInstallGC
	snappyAllowGadget

	snappyDeveloperMode
	snappyTryMode
)

const interimUnusableFlagValueTop = snapstate.InterimUnusableFlagValueLast << 1

func (s *flagscompatSuite) TestCopiedConstsSanity(c *C) {
	// have this sanity test at the start at least, can be dropped
	// when we drop snappy
	c.Check(snappy.LegacyInstallFlags(snappyAllowUnauthenticated), Equals, snappy.LegacyAllowUnauthenticated)
	c.Check(snappy.LegacyInstallFlags(snappyInhibitHooks), Equals, snappy.LegacyInhibitHooks)
	c.Check(snappy.LegacyInstallFlags(snappyDoInstallGC), Equals, snappy.LegacyDoInstallGC)
	c.Check(snappy.LegacyInstallFlags(snappyAllowGadget), Equals, snappy.LegacyAllowGadget)

	c.Check(snappy.LegacyInstallFlags(snappyDeveloperMode), Equals, snappy.InterimDeveloperMode)
	c.Check(snappy.LegacyInstallFlags(snappyTryMode), Equals, snappy.InterimTryMode)
}

func (s *flagscompatSuite) TestSnapSetupNewValuesUnchanged(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("t", "...")

	values := []int{
		snapstate.DevMode,
		snapstate.TryMode,
		snapstate.DevMode | snapstate.TryMode,
		interimUnusableFlagValueTop,
		interimUnusableFlagValueTop | snapstate.DevMode,
		interimUnusableFlagValueTop<<1 | snapstate.TryMode,
		interimUnusableFlagValueTop << 4,
	}

	for _, f := range values {

		t.Set("ss", snapstate.SnapSetup{
			Flags: snapstate.SnapSetupFlags(f),
		})

		var ss snapstate.SnapSetup
		err := t.Get("ss", &ss)
		c.Assert(err, IsNil)

		c.Check(ss.Flags, Equals, snapstate.SnapSetupFlags(f))
	}

}

func (s *flagscompatSuite) TestRangeCapturesLegacyInterim(c *C) {
	values := []int{
		// these overlap but weren't used in snapd actually
		//snappyAllowUnauthenticated,
		//snappyInhibitHooks,
		snappyDoInstallGC,
		snappyAllowGadget,
		snappyDeveloperMode,
		snappyTryMode,
	}

	for _, v := range values {
		c.Check(v < int(interimUnusableFlagValueTop), Equals, true)
		c.Check(v >= int(snapstate.InterimUnusableFlagValueMin), Equals, true)
	}

	c.Check(snappyDoInstallGC, Equals, snapstate.InterimUnusableFlagValueMin)
	c.Check(snappyTryMode, Equals, snapstate.InterimUnusableFlagValueLast)

}

func (s *flagscompatSuite) TestSnapSetupInterimValsUpgrade(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("t", "...")

	tests := []struct {
		interim, new int
	}{
		{snappyDeveloperMode, snapstate.DevMode},
		{snappyTryMode, snapstate.TryMode},
		{snappyDeveloperMode | snappyTryMode, snapstate.DevMode | snapstate.TryMode},
		{snappyDeveloperMode | snappyDoInstallGC, snapstate.DevMode},
		{snappyTryMode | snappyDoInstallGC, snapstate.TryMode},
		{snappyDeveloperMode | snappyTryMode | snappyDoInstallGC, snapstate.DevMode | snapstate.TryMode},
		{snappyDoInstallGC, 0},
		{interimUnusableFlagValueTop - 1, snapstate.DevMode | snapstate.TryMode},
	}

	for _, tst := range tests {

		t.Set("ss", snapstate.SnapSetup{
			Flags: snapstate.SnapSetupFlags(tst.interim),
		})

		var ss snapstate.SnapSetup
		err := t.Get("ss", &ss)
		c.Assert(err, IsNil)

		c.Check(ss.Flags, Equals, snapstate.SnapSetupFlags(tst.new))
	}

}
