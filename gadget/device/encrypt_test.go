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
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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

	c.Assert(os.MkdirAll(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, boot.InstallHostFDESaveDir), 0755), IsNil)

	// nothing was written yet
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"))), Equals, false)
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDESaveDir)), Equals, false)
	mylog.Check(device.WriteEncryptionMarkers(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), filepath.Join(d, boot.InstallHostFDESaveDir), []byte("foo")))

	// both markers were written
	c.Check(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "marker"), testutil.FileEquals, "foo")
	c.Check(filepath.Join(d, boot.InstallHostFDESaveDir, "marker"), testutil.FileEquals, "foo")
	// and can be read with device.ReadEncryptionMarkers
	m1, m2 := mylog.Check3(device.ReadEncryptionMarkers(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), filepath.Join(d, boot.InstallHostFDESaveDir)))

	c.Check(m1, DeepEquals, []byte("foo"))
	c.Check(m2, DeepEquals, []byte("foo"))
	// and are found via HasEncryptedMarkerUnder()
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"))), Equals, true)
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDESaveDir)), Equals, true)
}

func (s *deviceSuite) TestReadEncryptionMarkers(c *C) {
	tmpdir := c.MkDir()

	// simulate two different markers in "ubuntu-data" and "ubuntu-save"
	p1 := filepath.Join(tmpdir, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "marker")
	mylog.Check(os.MkdirAll(filepath.Dir(p1), 0755))

	mylog.Check(os.WriteFile(p1, []byte("marker-p1"), 0600))


	p2 := filepath.Join(tmpdir, boot.InstallHostFDESaveDir, "marker")
	mylog.Check(os.MkdirAll(filepath.Dir(p2), 0755))

	mylog.Check(os.WriteFile(p2, []byte("marker-p2"), 0600))


	// reading them returns the two different values
	m1, m2 := mylog.Check3(device.ReadEncryptionMarkers(filepath.Join(tmpdir, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), filepath.Join(tmpdir, boot.InstallHostFDESaveDir)))

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

	c.Check(device.TpmLockoutAuthUnder(dirs.SnapFDEDirUnderSave(dirs.SnapSaveDir)), Equals,
		"/var/lib/snapd/save/device/fde/tpm-lockout-auth")
}

func (s *deviceSuite) TestStampSealedKeysRunthrough(c *C) {
	root := c.MkDir()

	for _, tc := range []struct {
		mth      device.SealingMethod
		expected string
	}{
		{device.SealingMethodLegacyTPM, ""},
		{device.SealingMethodTPM, "tpm"},
		{device.SealingMethodFDESetupHook, "fde-setup-hook"},
	} {
		mylog.Check(device.StampSealedKeys(root, tc.mth))


		mth := mylog.Check2(device.SealedKeysMethod(root))

		c.Check(tc.mth, Equals, mth)

		content := mylog.Check2(os.ReadFile(filepath.Join(root, "/var/lib/snapd/device/fde/sealed-keys")))

		c.Check(string(content), Equals, tc.expected)
	}
}

func (s *deviceSuite) TestSealedKeysMethodWithMissingStamp(c *C) {
	root := c.MkDir()

	_ := mylog.Check2(device.SealedKeysMethod(root))
	c.Check(err, Equals, device.ErrNoSealedKeys)
}

func (s *deviceSuite) TestSealedKeysMethodWithWrongContentHappy(c *C) {
	root := c.MkDir()

	mockSealedKeyPath := filepath.Join(root, "/var/lib/snapd/device/fde/sealed-keys")
	mylog.Check(os.MkdirAll(filepath.Dir(mockSealedKeyPath), 0755))

	mylog.Check(os.WriteFile(mockSealedKeyPath, []byte("invalid-sealing-method"), 0600))


	// invalid/unknown sealing methods do not error
	mth := mylog.Check2(device.SealedKeysMethod(root))
	c.Check(err, IsNil)
	c.Check(string(mth), Equals, "invalid-sealing-method")
}
