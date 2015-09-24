// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&configSuite{})

type configSuite struct {
	common.SnappySuite
	backConfig string
}

func (s *configSuite) SetUpTest(c *check.C) {
	s.backConfig = currentConfig(c)
}

func (s *configSuite) TearDownTest(c *check.C) {
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

func (s *configSuite) doTest(c *check.C, targetCfg string) {
	config := configString(targetCfg)

	err := s.setConfig(c, config)
	c.Assert(err, check.IsNil)

	actualConfig := currentConfig(c)

	c.Assert(actualConfig, check.Matches, "(?Usm).*"+targetCfg+".*")
}

func (s *configSuite) setConfig(c *check.C, config string) (err error) {
	configFile, err := ioutil.TempFile("", "snappy-cfg")
	defer func() { configFile.Close(); os.Remove(configFile.Name()) }()
	if err != nil {
		return
	}
	_, err = configFile.Write([]byte(config))

	common.ExecCommand(c, "sudo", "snappy", "config", "ubuntu-core", configFile.Name())

	return
}

func currentConfig(c *check.C) string {
	return common.ExecCommand(c, "snappy", "config", "ubuntu-core")
}

func configString(cfg string) string {
	return `config:
  ubuntu-core:
    ` + cfg
}
