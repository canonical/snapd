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

package firstboot

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
)

func TestStore(t *testing.T) { TestingT(t) }

type FirstBootTestSuite struct {
	netplanConfigFile string
	enableConfig      []string
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)

	s.netplanConfigFile = netplanConfigFile
	netplanConfigFile = filepath.Join(c.MkDir(), "config.yaml")
	s.enableConfig = enableConfig
	enableConfig = []string{"/bin/true"}
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	netplanConfigFile = s.netplanConfigFile
	enableConfig = s.enableConfig
}

func (s *FirstBootTestSuite) TestInitialNetworkConfig(c *C) {
	c.Check(InitialNetworkConfig(), IsNil)
	bs, err := ioutil.ReadFile(netplanConfigFile)
	c.Assert(err, IsNil)
	c.Check(string(bs), Equals, netplanConfigData)
}

func (s *FirstBootTestSuite) TestInitialNetworkConfigBadPath(c *C) {
	netplanConfigFile = "/no/such/thing"
	err := InitialNetworkConfig()
	c.Check(err, NotNil)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *FirstBootTestSuite) TestInitialNetworkConfigEnableFails(c *C) {
	enableConfig = []string{"/bin/false"}
	err := InitialNetworkConfig()
	c.Check(err, NotNil)
	_, isExitError := err.(*exec.ExitError)
	c.Check(isExitError, Equals, true)
}
