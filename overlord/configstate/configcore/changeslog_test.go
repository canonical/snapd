// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

type changesLogSuite struct {
	configcoreSuite
}

var _ = Suite(&changesLogSuite{})

func (s *changesLogSuite) TestValidateChangesLogSettingTrue(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state:   s.state,
		changes: map[string]any{"system.enable-changes-log": true},
	})
	c.Assert(err, IsNil)
}

func (s *changesLogSuite) TestValidateChangesLogSettingFalse(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state:   s.state,
		changes: map[string]any{"system.enable-changes-log": false},
	})
	c.Assert(err, IsNil)
}

func (s *changesLogSuite) TestValidateChangesLogSettingInvalid(c *C) {
	err := configcore.Run(classicDev, &mockConf{
		state:   s.state,
		changes: map[string]any{"system.enable-changes-log": "invalid"},
	})
	c.Assert(err, ErrorMatches, `system.enable-changes-log can only be set to 'true' or 'false'`)
}
