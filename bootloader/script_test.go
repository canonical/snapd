// -*- Mode: Go; indent-tabs-mode: t -*-

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

package bootloader_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
)

type scriptTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&scriptTestSuite{})

func (s *scriptTestSuite) TestTrivialFromScript(c *C) {
	e, err := bootloader.EditionFromScript(bytes.NewBufferString(`# X-Snapd-boot-script-edition: 321
next line
one after that`))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(321))
}

func (s *scriptTestSuite) TestTrivialFromFile(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	ioutil.WriteFile(p, []byte(`# X-Snapd-boot-script-edition: 123
this is some
this too`), 0644)
	e, err := bootloader.EditionFromScriptFile(p)
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(123))
}

func (s *scriptTestSuite) TestRealScript(c *C) {
	grubScript := assets.GetBootAsset("grub.conf")
	c.Assert(grubScript, NotNil)
	e, err := bootloader.EditionFromScript(bytes.NewReader(grubScript))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(1))
}

func (s *scriptTestSuite) TestNoScript(c *C) {
	_, err := bootloader.EditionFromScript(bytes.NewReader(nil))
	c.Assert(err, ErrorMatches, "cannot read boot script: unexpected EOF")
}

func (s *scriptTestSuite) TestNoFile(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	// file does not exist
	_, err := bootloader.EditionFromScriptFile(p)
	c.Assert(err, ErrorMatches, "no edition")
}

func (s *scriptTestSuite) TestUnreadableFile(c *C) {
	// root has DAC override
	if os.Geteuid() == 0 {
		c.Skip("test case cannot be correctly executed by root")
	}
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	err := ioutil.WriteFile(p, []byte("foo"), 0000)
	c.Assert(err, IsNil)
	_, err = bootloader.EditionFromScriptFile(p)
	c.Assert(err, ErrorMatches, "cannot load existing boot script: .*/foo: permission denied")
}

func (s *scriptTestSuite) TestNoEdition(c *C) {
	_, err := bootloader.EditionFromScript(bytes.NewReader([]byte(`this is some script
without edition header
`)))
	c.Assert(err, ErrorMatches, "no edition")
}

func (s *scriptTestSuite) TestBadEdition(c *C) {
	_, err := bootloader.EditionFromScript(bytes.NewReader([]byte(`# X-Snapd-boot-script-edition: random
data follows
`)))
	c.Assert(err, ErrorMatches, `cannot parse script edition: .* parsing "random": invalid syntax`)
}

func (s *scriptTestSuite) TestBootScriptFrom(c *C) {
	script := []byte(`# X-Snapd-boot-script-edition: 123
data follows
`)
	bs, err := bootloader.BootScriptFrom(script)
	c.Assert(err, IsNil)
	c.Assert(bs, NotNil)
	c.Assert(bs.Edition(), Equals, uint(123))
	c.Assert(bs.Script(), DeepEquals, script)
}
