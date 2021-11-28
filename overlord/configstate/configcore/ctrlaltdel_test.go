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

package configcore_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/snap"
)

type unitStates int

// The possible unit states we should test for to make sure
// appropriate error messages are displayed
const (
	unitStateNone unitStates = iota
	unitStateMulti
	unitStateUninstalled
	unitStateDisabled
	unitStateEnabled
	// Ubuntu Core <= 18 has an earlier version of systemd and the
	// UnitFileState for a masked unit is returned as 'bad'.
	// LoadState (unused by us) returns 'masked'.
	unitStateMaskedv1
	// Ubuntu Core > 18 has a later version of systemd and the
	// UnitFileState for a masked unit is returned as 'masked'.
	unitStateMaskedv2
)

type ctrlaltdelSuite struct {
	configcoreSuite
	unit unitStates
}

var _ = Suite(&ctrlaltdelSuite{})

func (s *ctrlaltdelSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	s.systemctlOutput = func(args ...string) []byte {
		var output []byte
		if args[0] == "show" {
			switch s.unit {
			case unitStateMulti:
				output = []byte("Id=ctrl-alt-del.target\nActiveState=inactive\nUnitFileState=enabled\nNames=ctrl-alt-del.target\n" +
					"\n" +
					fmt.Sprintf("Id=%s\nActiveState=inactive\nUnitFileState=enabled\nNames=%[1]s\n", args[2]))
			case unitStateUninstalled:
				output = []byte(fmt.Sprintf("Id=%s\nActiveState=inactive\nUnitFileState=\nNames=%[1]s\n", args[2]))
			case unitStateDisabled:
				output = []byte(fmt.Sprintf("Id=%s\nActiveState=inactive\nUnitFileState=disabled\nNames=%[1]s\n", args[2]))
			case unitStateEnabled:
				output = []byte(fmt.Sprintf("Id=%s\nActiveState=inactive\nUnitFileState=enabled\nNames=%[1]s\n", args[2]))
			case unitStateMaskedv1:
				output = []byte(fmt.Sprintf("Id=%s\nActiveState=inactive\nUnitFileState=bad\nNames=%[1]s\n", args[2]))
			case unitStateMaskedv2:
				output = []byte(fmt.Sprintf("Id=%s\nActiveState=inactive\nUnitFileState=masked\nNames=%[1]s\n", args[2]))
			default:
				// No output returned by systemctl
			}
		}
		return output
	}
	s.unit = unitStateNone
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

// Only "none" or "reboot" are valid action states
func (s *ctrlaltdelSuite) TestCtrlAltDelInvalidAction(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "xxx",
		},
	})
	c.Check(err, ErrorMatches, `invalid action "xxx" supplied for system.ctrl-alt-del-action option`)
}

// Only the status properties of a single matching unit (ctrl-alt-del.target) is expected
func (s *ctrlaltdelSuite) TestCtrlAltDelInvalidSystemctlReply(c *C) {
	s.unit = unitStateMulti
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "none",
		},
	})
	c.Check(err, ErrorMatches, `cannot get unit status: expected 1 results, got 2`)
}

// The ctrl-alt-del.target unit is expected to be installed in the filesystem
func (s *ctrlaltdelSuite) TestCtrlAltDelInvalidInstalledState(c *C) {
	s.unit = unitStateUninstalled
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "none",
		},
	})
	c.Check(err, ErrorMatches, `internal error: target ctrl-alt-del.target not installed`)
}

// The ctrl-alt-del.target unit may not be in the enabled state
func (s *ctrlaltdelSuite) TestCtrlAltDelInvalidEnabledState(c *C) {
	s.unit = unitStateEnabled
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "none",
		},
	})
	c.Check(err, ErrorMatches, `internal error: target ctrl-alt-del.target should not be enabled`)
}

// The ctrl-alt-del.target unit may be in the disabled state (reboot action)
func (s *ctrlaltdelSuite) TestCtrlAltDelValidDisabledState(c *C) {
	var err error
	s.unit = unitStateDisabled
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "reboot",
		},
	})
	c.Assert(err, IsNil)
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "none",
		},
	})
	c.Assert(err, IsNil)
}

// The ctrl-alt-del.target unit may be in the masked state (none action). This
// test represents the UnitFileState as returned for Ubuntu Core 16 and 18
func (s *ctrlaltdelSuite) TestCtrlAltDelValidMaskedStatev1(c *C) {
	var err error
	s.unit = unitStateMaskedv1
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "reboot",
		},
	})
	c.Assert(err, IsNil)
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "none",
		},
	})
	c.Assert(err, IsNil)
}

// The ctrl-alt-del.target unit may be in the masked state (none action). This
// test represents the UnitFileState as returned for Ubuntu Core 20 and later
func (s *ctrlaltdelSuite) TestCtrlAltDelValidMaskedStatev2(c *C) {
	var err error
	s.unit = unitStateMaskedv2
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "reboot",
		},
	})
	c.Assert(err, IsNil)
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.ctrl-alt-del-action": "none",
		},
	})
	c.Assert(err, IsNil)
}
