// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

/*
 * Copyright (C) 2017-2022 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type storeSuite struct {
	configcoreSuite
}

var _ = Suite(&storeSuite{})

func (s *storeSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/etc/environment"), nil, 0644)
	c.Assert(err, IsNil)
}

func (s *storeSuite) TestValidateStoreAccessHappy(c *C) {
	for _, access := range []string{"offline", "online"} {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"store.access": access,
			},
		})
		c.Assert(err, IsNil)
	}
}

func (s *storeSuite) TestValidateStoreAccessUnhappy(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store.access": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, ".*store access can only be set to 'online' or 'offline'$")
}
