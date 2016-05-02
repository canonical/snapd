// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

package tests

import (
	"io/ioutil"
	"os"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&configSuite{})

type configSuite struct {
	common.SnappySuite
	osSnap     string
	backConfig string
}

func (s *configSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)
	c.Skip("FIXME: port to snap")
	s.osSnap = partition.OSSnapName(c)
	s.backConfig = currentConfig(c, s.osSnap)
}

func (s *configSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)
	s.setConfig(c, s.backConfig)
}

func (s *configSuite) TestModprobe(c *check.C) {
	s.doTest(c, `modprobe: blacklist floppy`)
}

func (s *configSuite) TestPPP(c *check.C) {
	s.doTest(c, `network:
      ppp:
      - name: chap-secrets
        content: secret`)
}

func (s *configSuite) TestWatchdog(c *check.C) {
	s.doTest(c, `watchdog:
      startup: |
        run_watchdog=0
        run_wd_keepalive=0
        watchdog_module="none"`)
}

func (s *configSuite) TestNetworkInterfaces(c *check.C) {
	s.doTest(c, `network:
      interfaces:
      - name: eth0
        content: config`)
}

func (s *configSuite) doTest(c *check.C, targetCfg string) {
	config := configString(targetCfg)

	err := s.setConfig(c, config)
	c.Assert(err, check.IsNil, check.Commentf("Error setting config: %s", err))

	actualConfig := currentConfig(c, s.osSnap)

	// we admit any characters after a new line, this is needed because the reuse
	// of the config yaml string in the regex pattern, after setting a configuration
	// option any other sibling options can appear before the one set when displaying
	// the configuration (for instance, network-interfaces is always shown before
	// network-ppp)
	configPattern := "(?Usm).*" + strings.Replace(targetCfg, "\n", "\n.*", -1) + ".*"
	c.Assert(actualConfig, check.Matches, configPattern)
}

func (s *configSuite) setConfig(c *check.C, config string) (err error) {
	configFile, err := ioutil.TempFile("", "snappy-cfg")
	defer func() { configFile.Close(); os.Remove(configFile.Name()) }()
	if err != nil {
		return
	}
	_, err = configFile.Write([]byte(config))

	cli.ExecCommand(c, "sudo", "snappy", "config", s.osSnap, configFile.Name())

	return
}

func currentConfig(c *check.C, osSnap string) string {
	return cli.ExecCommand(c, "sudo", "snappy", "config", osSnap)
}

func configString(cfg string) string {
	return `config:
  ubuntu-core:
    ` + cfg
}
