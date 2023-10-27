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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
)

type configAssetTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&configAssetTestSuite{})

func (s *configAssetTestSuite) TestTrivialFromConfigAssert(c *C) {
	e, err := bootloader.EditionFromConfigAsset(bytes.NewBufferString(`# Snapd-Boot-Config-Edition: 321
next line
one after that`))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(321))

	e, err = bootloader.EditionFromConfigAsset(bytes.NewBufferString(`# Snapd-Boot-Config-Edition: 932
# Snapd-Boot-Config-Edition: 321
one after that`))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(932))

	e, err = bootloader.EditionFromConfigAsset(bytes.NewBufferString(`# Snapd-Boot-Config-Edition: 1234
one after that
# Snapd-Boot-Config-Edition: 321
`))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(1234))
}

func (s *configAssetTestSuite) TestTrivialFromFile(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	os.WriteFile(p, []byte(`# Snapd-Boot-Config-Edition: 123
this is some
this too`), 0644)
	e, err := bootloader.EditionFromDiskConfigAsset(p)
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(123))
}

func (s *configAssetTestSuite) TestRealConfig(c *C) {
	grubConfig := assets.Internal("grub.cfg")
	c.Assert(grubConfig, NotNil)
	e, err := bootloader.EditionFromConfigAsset(bytes.NewReader(grubConfig))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(3))
}

func (s *configAssetTestSuite) TestRealRecoveryConfig(c *C) {
	grubRecoveryConfig := assets.Internal("grub-recovery.cfg")
	c.Assert(grubRecoveryConfig, NotNil)
	e, err := bootloader.EditionFromConfigAsset(bytes.NewReader(grubRecoveryConfig))
	c.Assert(err, IsNil)
	c.Assert(e, Equals, uint(2))
}

func (s *configAssetTestSuite) TestNoConfig(c *C) {
	_, err := bootloader.EditionFromConfigAsset(bytes.NewReader(nil))
	c.Assert(err, ErrorMatches, "cannot read config asset: unexpected EOF")
}

func (s *configAssetTestSuite) TestNoFile(c *C) {
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	// file does not exist
	_, err := bootloader.EditionFromDiskConfigAsset(p)
	c.Assert(err, ErrorMatches, "no edition")
}

func (s *configAssetTestSuite) TestUnreadableFile(c *C) {
	// root has DAC override
	if os.Geteuid() == 0 {
		c.Skip("test case cannot be correctly executed by root")
	}
	d := c.MkDir()
	p := filepath.Join(d, "foo")
	err := os.WriteFile(p, []byte("foo"), 0000)
	c.Assert(err, IsNil)
	_, err = bootloader.EditionFromDiskConfigAsset(p)
	c.Assert(err, ErrorMatches, "cannot load existing config asset: .*/foo: permission denied")
}

func (s *configAssetTestSuite) TestNoEdition(c *C) {
	_, err := bootloader.EditionFromConfigAsset(bytes.NewReader([]byte(`this is some script
without edition header
`)))
	c.Assert(err, ErrorMatches, "no edition")
}

func (s *configAssetTestSuite) TestBadEdition(c *C) {
	_, err := bootloader.EditionFromConfigAsset(bytes.NewReader([]byte(`# Snapd-Boot-Config-Edition: random
data follows
`)))
	c.Assert(err, ErrorMatches, `cannot parse asset edition: .* parsing "random": invalid syntax`)
}

func (s *configAssetTestSuite) TestConfigAssetFrom(c *C) {
	script := []byte(`# Snapd-Boot-Config-Edition: 123
data follows
`)
	bs, err := bootloader.ConfigAssetFrom(script)
	c.Assert(err, IsNil)
	c.Assert(bs, NotNil)
	c.Assert(bs.Edition(), Equals, uint(123))
	c.Assert(bs.Raw(), DeepEquals, script)
}
