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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type securitySuite struct {
	configcoreSuite
}

var _ = Suite(&securitySuite{})

func (s *securitySuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
}

func (s *securitySuite) TestSecurityValidation(c *C) {
	key := "system.security.required-publisher-validations"
	// Test valid cases
	for _, val := range []any{"verified", "starred", "certified", "verified,starred", " verified, certified ", ""} {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]any{
				key: val,
			},
		})
		c.Assert(err, IsNil, Commentf("key: %s, val: %v", key, val))
	}

	// Test invalid cases
	for _, val := range []any{"invalid", "verified,invalid"} {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]any{
				key: val,
			},
		})
		c.Assert(err, ErrorMatches, "unsupported publisher validation: .*")
	}
}
