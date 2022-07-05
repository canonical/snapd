// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package device_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/testutil"
)

func TestT(t *testing.T) {
	TestingT(t)
}

type deviceSuite struct{}

var _ = Suite(&deviceSuite{})

func (s *deviceSuite) TestEncryptionMarkersRunThrough(c *C) {
	d := c.MkDir()
	c.Check(device.HasEncryptedMarkerUnder(d), Equals, false)

	c.Assert(os.MkdirAll(filepath.Join(d, boot.InstallHostFDEDataDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, boot.InstallHostFDESaveDir), 0755), IsNil)

	// nothing was written yet
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDEDataDir)), Equals, false)
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDESaveDir)), Equals, false)

	err := device.WriteEncryptionMarkers(filepath.Join(d, boot.InstallHostFDEDataDir), filepath.Join(d, boot.InstallHostFDESaveDir), []byte("foo"))
	c.Assert(err, IsNil)
	// both markers were written
	c.Check(filepath.Join(d, boot.InstallHostFDEDataDir, "marker"), testutil.FileEquals, "foo")
	c.Check(filepath.Join(d, boot.InstallHostFDESaveDir, "marker"), testutil.FileEquals, "foo")
	// and can be read with device.ReadEncryptionMarkers
	m1, m2, err := device.ReadEncryptionMarkers(filepath.Join(d, boot.InstallHostFDEDataDir), filepath.Join(d, boot.InstallHostFDESaveDir))
	c.Assert(err, IsNil)
	c.Check(m1, DeepEquals, []byte("foo"))
	c.Check(m2, DeepEquals, []byte("foo"))
	// and are found via HasEncryptedMarkerUnder()
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDEDataDir)), Equals, true)
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDESaveDir)), Equals, true)
}

func (s *deviceSuite) TestReadEncryptionMarkers(c *C) {
	tmpdir := c.MkDir()

	// simulate two different markers in "ubuntu-data" and "ubuntu-save"
	p1 := filepath.Join(tmpdir, boot.InstallHostFDEDataDir, "marker")
	err := os.MkdirAll(filepath.Dir(p1), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(p1, []byte("marker-p1"), 0600)
	c.Assert(err, IsNil)

	p2 := filepath.Join(tmpdir, boot.InstallHostFDESaveDir, "marker")
	err = os.MkdirAll(filepath.Dir(p2), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(p2, []byte("marker-p2"), 0600)
	c.Assert(err, IsNil)

	// reading them returns the two different values
	m1, m2, err := device.ReadEncryptionMarkers(filepath.Join(tmpdir, boot.InstallHostFDEDataDir), filepath.Join(tmpdir, boot.InstallHostFDESaveDir))
	c.Assert(err, IsNil)
	c.Check(m1, DeepEquals, []byte("marker-p1"))
	c.Check(m2, DeepEquals, []byte("marker-p2"))
}

func (s *deviceSuite) TestLocations(c *C) {
	c.Check(device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir), Equals,
		"/run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key")
	c.Check(device.SaveKeyUnder(dirs.SnapFDEDir), Equals,
		"/var/lib/snapd/device/fde/ubuntu-save.key")
	c.Check(device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir), Equals,
		"/run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key")
	c.Check(device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir), Equals,
		"/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key")
	c.Check(device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir), Equals,
		"/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key.factory-reset")
}
