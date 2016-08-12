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

package provisioning

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/partition"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ProvisioningTestSuite struct {
	mockYamlFile string

	bootloader *boottest.MockBootloader
}

var _ = Suite(&ProvisioningTestSuite{})

var yamlData = `
meta:
  timestamp: 2015-04-20T14:15:39.013515821+01:00

tool:
  name: ubuntu-device-flash
  path: /usr/bin/ubuntu-device-flash
  version: ""

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

tool:
  name: ubuntu-device-flash
  path: /usr/bin/ubuntu-device-flash
  version: ""

options:
  size: 3
  size-unit: GB
  output: /tmp/bbb.img
  channel: ubuntu-core/devel-proposed
  developer-mode: true
`

var garbageData = `Fooled you!?`

func (ts *ProvisioningTestSuite) SetUpTest(c *C) {
	ts.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	ts.mockYamlFile = filepath.Join(ts.bootloader.Dir(), "install.yaml")

	findBootloader = func() (partition.Bootloader, error) {
		return ts.bootloader, nil
	}
}

func (ts *ProvisioningTestSuite) TearDownTest(c *C) {
	findBootloader = partition.FindBootloader
}

func (ts *ProvisioningTestSuite) TestParseInstallYaml(c *C) {

	_, err := parseInstallYaml(ts.mockYamlFile)
	c.Check(err, ErrorMatches, `failed to read provisioning data: open .*/install.yaml: no such file or directory`)

	err = ioutil.WriteFile(ts.mockYamlFile, []byte(yamlData), 0750)
	c.Check(err, IsNil)
	_, err = parseInstallYaml(ts.mockYamlFile)
	c.Check(err, IsNil)

	err = ioutil.WriteFile(ts.mockYamlFile, []byte(yamlDataNoDevicePart), 0750)
	c.Check(err, IsNil)
	_, err = parseInstallYaml(ts.mockYamlFile)
	c.Check(err, IsNil)

	err = ioutil.WriteFile(ts.mockYamlFile, []byte(garbageData), 0750)
	c.Check(err, IsNil)
	_, err = parseInstallYaml(ts.mockYamlFile)
	c.Check(err, Not(Equals), nil)
}

func (ts *ProvisioningTestSuite) TestParseInstallYamlData(c *C) {

	_, err := parseInstallYamlData([]byte(""))
	c.Check(err, IsNil)

	_, err = parseInstallYamlData([]byte(yamlData))
	c.Check(err, IsNil)

	_, err = parseInstallYamlData([]byte(yamlDataNoDevicePart))
	c.Check(err, IsNil)

	_, err = parseInstallYamlData([]byte(garbageData))
	c.Check(err, Not(Equals), nil)
}

func (ts *ProvisioningTestSuite) TestInDeveloperModeWithDevModeOn(c *C) {
	err := ioutil.WriteFile(ts.mockYamlFile, []byte(`
options:
 developer-mode: true
`), 0644)
	c.Assert(err, IsNil)
	c.Assert(InDeveloperMode(), Equals, true)
}

func (ts *ProvisioningTestSuite) TestInDeveloperModeWithDevModeOff(c *C) {
	err := ioutil.WriteFile(ts.mockYamlFile, []byte(`
options:
 developer-mode: false
`), 0644)
	c.Assert(err, IsNil)
	c.Assert(InDeveloperMode(), Equals, false)
}
