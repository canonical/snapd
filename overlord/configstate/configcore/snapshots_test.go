// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type snapshotsSuite struct {
	configcoreSuite
}

var _ = Suite(&snapshotsSuite{})

func (s *snapshotsSuite) TestConfigureAutomaticSnapshotsExpirationHappy(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"snapshots.automatic.retention": "40h",
		},
	}))

}

func (s *snapshotsSuite) TestConfigureAutomaticSnapshotsExpirationTooLow(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"snapshots.automatic.retention": "10m",
		},
	}))
	c.Assert(err, ErrorMatches, `snapshots.automatic.retention must be a value greater than 24 hours, or "no" to disable`)
}

func (s *snapshotsSuite) TestConfigureAutomaticSnapshotsDisable(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"snapshots.automatic.retention": "no",
		},
	}))

}

func (s *refreshSuite) TestConfigureAutomaticSnapshotsExpirationInvalid(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"snapshots.automatic.retention": "invalid",
		},
	}))
	c.Assert(err, ErrorMatches, `snapshots.automatic.retention cannot be parsed:.*`)
}
