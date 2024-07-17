// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type coredumpSuite struct {
	configcoreSuite

	coredumpCfgDir  string
	coredumpCfgPath string
}

var _ = Suite(&coredumpSuite{})

func (s *coredumpSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	s.coredumpCfgDir = filepath.Join(dirs.SnapSystemdDir, "coredump.conf.d")
	s.coredumpCfgPath = filepath.Join(s.coredumpCfgDir, "ubuntu-core.conf")
}

func (s *coredumpSuite) TestConfigureCoredumpDefault(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{},
	})
	c.Assert(err, IsNil)

	c.Check(s.coredumpCfgPath, testutil.FileEquals,
		fmt.Sprintf("[Coredump]\nStorage=none\nProcessSizeMax=0\n"))
}

func (s *coredumpSuite) TestConfigureCoredumpDisable(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.coredump.enable": false,
			"system.coredump.maxuse": "100M",
		},
	})
	c.Assert(err, IsNil)

	c.Check(s.coredumpCfgPath, testutil.FileEquals,
		fmt.Sprintf("[Coredump]\nStorage=none\nProcessSizeMax=0\n"))
}

func (s *coredumpSuite) TestConfigureCoredumpDefaultMaxUse(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.coredump.enable": true,
		},
	})
	c.Assert(err, IsNil)

	c.Check(s.coredumpCfgPath, testutil.FileEquals,
		fmt.Sprintf("[Coredump]\nStorage=external\n"))
}

func (s *coredumpSuite) TestConfigureCoredumpWithMaxUse(c *C) {
	// Configure with different MaxUse valid values
	for _, size := range []string{"104857600", "16M", "2G", "0"} {
		err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.coredump.enable": true,
				"system.coredump.maxuse": size,
			},
		})
		c.Assert(err, IsNil)

		c.Check(s.coredumpCfgPath, testutil.FileEquals,
			fmt.Sprintf("[Coredump]\nStorage=external\nMaxUse=%s\n", size))
	}
}

func (s *coredumpSuite) TestConfigureCoredumpInvalidMaxUse(c *C) {
	// Configure with different MaxUse invalid values
	for _, size := range []string{"100p", "0x123", "10485f7600", "20%%",
		"20%", "100m", "10k", "10K", "10g"} {

		err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.coredump.enable": true,
				"system.coredump.maxuse": size,
			},
		})
		c.Assert(err, ErrorMatches, `invalid suffix .*`)

		c.Assert(s.coredumpCfgPath, testutil.FileAbsent)
	}
}
