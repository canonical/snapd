// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

type snapshotsSuite struct {
	configcoreSuite
}

var _ = Suite(&snapshotsSuite{})

func (s *snapshotsSuite) TestConfigureAutomaticSnapshotsExpirationHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"automatic-snapshots.expiration": "40h",
		},
	})
	c.Assert(err, IsNil)
}

func (s *snapshotsSuite) TestConfigureAutomaticSnapshotsExpirationTooLow(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"automatic-snapshots.expiration": "10m",
		},
	})
	c.Assert(err, ErrorMatches, `automatic-snapshots.expiration must be 0 to disable automatic snapshots, or a value greater than 24 hours`)
}

func (s *snapshotsSuite) TestConfigureAutomaticSnapshotsDisable(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"automatic-snapshots.expiration": "0",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureAutomaticSnapshotsExpirationInvalid(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"automatic-snapshots.expiration": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `automatic-snapshots.expiration cannot be parsed:.*`)
}
