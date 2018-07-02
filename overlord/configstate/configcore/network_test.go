// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type networkSuite struct {
	configcoreSuite

	mockNetworkSysctlPath string
	restores              []func()
	mockSysctl            *testutil.MockCmd
}

var _ = Suite(&networkSuite{})

func (s *networkSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.restores = append(s.restores, release.MockOnClassic(false))

	s.mockSysctl = testutil.MockCommand(c, "sysctl", "")
	s.restores = append(s.restores, func() { s.mockSysctl.Restore() })

	s.mockNetworkSysctlPath = filepath.Join(dirs.GlobalRootDir, "/etc/sysctl.d/10-snapd-network.conf")
	c.Assert(os.MkdirAll(filepath.Dir(s.mockNetworkSysctlPath), 0755), IsNil)
}

func (s *networkSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	for _, f := range s.restores {
		f()
	}
}

func (s *networkSuite) TestConfigureNetworkIntegrationIPv6True(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"network.disable-ipv6": true,
		},
	})
	c.Assert(err, IsNil)

	c.Check(s.mockNetworkSysctlPath, testutil.FileEquals, "net.ipv6.conf.all.disable_ipv6=1\n")
	c.Check(s.mockSysctl.Calls(), DeepEquals, [][]string{
		{"sysctl", "-p", s.mockNetworkSysctlPath},
	})
}

func (s *networkSuite) TestConfigureNetworkIntegrationIPv6False(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"network.disable-ipv6": false,
		},
	})
	c.Assert(err, IsNil)

	c.Check(s.mockNetworkSysctlPath, testutil.FileEquals, "net.ipv6.conf.all.disable_ipv6=0\n")
	c.Check(s.mockSysctl.Calls(), DeepEquals, [][]string{
		{"sysctl", "-p", s.mockNetworkSysctlPath},
	})
}

func (s *networkSuite) TestConfigureNetworkIntegrationNone(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf:  map[string]interface{}{},
	})
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(s.mockNetworkSysctlPath), Equals, false)
}
