/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

type ProvisioningTestSuite struct {
	tempdir         string
	installYamlFile string
}

var _ = Suite(&ProvisioningTestSuite{})

var yamlData = `
meta:
  timestamp: 2015-04-20T14:15:39.013515821+01:00
  initial-revision: r345
  system-image-server: http://system-image.ubuntu.com

tool:
  name: ubuntu-device-flash
  path: /usr/bin/ubuntu-device-flash
  version: 0.20snappy3-0ubuntu1

options:
  size: 3
  size-unit: GB
  output: /tmp/bbb.img
  channel: ubuntu-core/devel-proposed
  device-part: /some/path/file.tgz
  developer-mode: true
`

var yamlDataNoDevicePart = `
meta:
  timestamp: 2015-04-20T14:15:39.013515821+01:00
  initial-revision: r345
  system-image-server: http://system-image.ubuntu.com

tool:
  name: ubuntu-device-flash
  path: /usr/bin/ubuntu-device-flash
  version: 0.20snappy3-0ubuntu1

options:
  size: 3
  size-unit: GB
  output: /tmp/bbb.img
  channel: ubuntu-core/devel-proposed
  developer-mode: true
`

var garbageData = `Fooled you!?`

func (ts *ProvisioningTestSuite) SetUpTest(c *C) {
	ts.tempdir = c.MkDir()

	ts.installYamlFile = filepath.Join(ts.tempdir, "install.yaml")

	InstallYamlFile = ts.installYamlFile
	os.Remove(InstallYamlFile)
}

func (ts *ProvisioningTestSuite) TearDownTest(c *C) {
	os.Remove(InstallYamlFile)
}

func (ts *ProvisioningTestSuite) TestSideLoadedSystemNoInstallYaml(c *C) {
	c.Assert(sideLoadedSystem(), Equals, false)
}

func (ts *ProvisioningTestSuite) TestSideLoadedSystem(c *C) {
	c.Assert(sideLoadedSystem(), Equals, false)

	err := ioutil.WriteFile(InstallYamlFile, []byte(yamlData), 0750)
	c.Assert(err, IsNil)

	c.Assert(sideLoadedSystem(), Equals, true)

	os.Remove(InstallYamlFile)
	c.Assert(sideLoadedSystem(), Equals, false)
}

func (ts *ProvisioningTestSuite) TestSideLoadedSystemNoDevicePart(c *C) {

	c.Assert(sideLoadedSystem(), Equals, false)

	err := ioutil.WriteFile(InstallYamlFile, []byte(yamlDataNoDevicePart), 0750)
	c.Assert(err, IsNil)

	c.Assert(sideLoadedSystem(), Equals, false)

	os.Remove(InstallYamlFile)
	c.Assert(sideLoadedSystem(), Equals, false)
}

func (ts *ProvisioningTestSuite) TestSideLoadedSystemGarbageInstallYaml(c *C) {
	c.Assert(sideLoadedSystem(), Equals, false)

	err := ioutil.WriteFile(InstallYamlFile, []byte(garbageData), 0750)
	c.Assert(err, IsNil)

	c.Assert(sideLoadedSystem(), Equals, false)

	os.Remove(InstallYamlFile)
	c.Assert(sideLoadedSystem(), Equals, false)
}

func (ts *ProvisioningTestSuite) TestParseInstallYaml(c *C) {

	_, err := parseInstallYaml(InstallYamlFile)
	c.Assert(err, Equals, ErrNoInstallYaml)

	err = ioutil.WriteFile(InstallYamlFile, []byte(yamlData), 0750)
	c.Assert(err, IsNil)
	_, err = parseInstallYaml(InstallYamlFile)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(InstallYamlFile, []byte(yamlDataNoDevicePart), 0750)
	c.Assert(err, IsNil)
	_, err = parseInstallYaml(InstallYamlFile)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(InstallYamlFile, []byte(garbageData), 0750)
	c.Assert(err, IsNil)
	_, err = parseInstallYaml(InstallYamlFile)
	c.Assert(err, Not(Equals), nil)
}

func (ts *ProvisioningTestSuite) TestParseInstallYamlData(c *C) {

	_, err := parseInstallYamlData([]byte(""))
	c.Assert(err, IsNil)

	_, err = parseInstallYamlData([]byte(yamlData))
	c.Assert(err, IsNil)

	_, err = parseInstallYamlData([]byte(yamlDataNoDevicePart))
	c.Assert(err, IsNil)

	_, err = parseInstallYamlData([]byte(garbageData))
	c.Assert(err, Not(Equals), nil)
}
