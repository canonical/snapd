// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2025 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type interfaceSuite struct {
	configcoreSuite
}

var _ = Suite(&interfaceSuite{})

func (s *interfaceSuite) TestConfigureInterfaceUnhappyName(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.invalid.test": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `unsupported interface "invalid" for configuration change`)
}

func (s *interfaceSuite) TestConfigureInterfaceUnsupportedBuiltinName(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.network.test": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `unsupported interface "network" for configuration change`)
}

func (s *interfaceSuite) TestConfigureInterfaceUnhappyIncompleteName(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `interface must be specified for "core.interface."`)
}

func (s *interfaceSuite) TestConfigureInterfaceUnhappyOption(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.x11.test": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `unsupported interface option: "test"`)
}

func (s *interfaceSuite) TestConfigureInterfaceUnhappyIncompleteOption(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.x11.": "xxx",
		},
	})
	c.Assert(err, ErrorMatches, `interface option must be specified for "core.interface.x11."`)
}

func (s *interfaceSuite) TestConfigureInterfaceUnhappyOptionValue(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.x11.allow-auto-connection": "test",
		},
	})
	c.Assert(err, ErrorMatches, `core.interface.x11.allow-auto-connection can only be set to 'true', 'false' or 'verified'`)
}

func (s *interfaceSuite) TestConfigureInterfaceHappy(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"interface.x11.allow-auto-connection": "true",
		},
	})
	c.Assert(err, IsNil)
}
