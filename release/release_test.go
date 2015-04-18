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

package release

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ReleaseTestSuite struct {
	root  string
	siDir string
}

var _ = Suite(&ReleaseTestSuite{})

const siFmt = `[service]
channel: %s
`

func (s *ReleaseTestSuite) writeChannelInformation(channelInfo string) error {
	si := fmt.Sprintf(siFmt, channelInfo)
	return ioutil.WriteFile(filepath.Join(s.siDir, "channel.ini"), []byte(si), 0644)
}

func (s *ReleaseTestSuite) SetUpTest(c *C) {
	s.root = c.MkDir()
	s.siDir = filepath.Join(s.root, "etc", "system-image")
	c.Assert(os.MkdirAll(s.siDir, 0755), IsNil)
}

func (s *ReleaseTestSuite) TestSetup(c *C) {
	c.Assert(s.writeChannelInformation("ubuntu-core/15.04/edge"), IsNil)

	c.Assert(Setup(s.root), IsNil)
	c.Check(String(), Equals, "15.04-core")
	c.Check(rel.Flavor, Equals, "core")
	c.Check(rel.Series, Equals, "15.04")
	c.Check(rel.Channel, Equals, "edge")
}

func (s *ReleaseTestSuite) TestOverride(c *C) {
	Override(Release{Flavor: "personal", Series: "10.06", Channel: "beta"})
	c.Check(String(), Equals, "10.06-personal")
	c.Check(rel.Flavor, Equals, "personal")
	c.Check(rel.Series, Equals, "10.06")
	c.Check(rel.Channel, Equals, "beta")
}

func (s *ReleaseTestSuite) TestSetupLegacyChannels(c *C) {
	c.Assert(s.writeChannelInformation("ubuntu-core/devel"), IsNil)

	c.Assert(Setup(s.root), IsNil)
	c.Check(String(), Equals, "15.04-core")
	c.Check(rel.Flavor, Equals, "core")
	c.Check(rel.Series, Equals, "15.04")
	c.Check(rel.Channel, Equals, "")
}

func (s *ReleaseTestSuite) TestNoChannelErrors(c *C) {
	c.Assert(Setup(s.root), NotNil)
}

func (s *ReleaseTestSuite) TestNoUbuntuChannelErrors(c *C) {
	c.Assert(s.writeChannelInformation("kubuntu-core/devel/beta"), IsNil)
	c.Assert(Setup(s.root), NotNil)
}

func (s *ReleaseTestSuite) TestNoServiceInChannelIni(c *C) {
	c.Assert(ioutil.WriteFile(filepath.Join(s.siDir, "channel.ini"), []byte{0}, 0644), IsNil)
	c.Assert(Setup(s.root), NotNil)
}
