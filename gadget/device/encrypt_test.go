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
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	c.Assert(os.MkdirAll(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, boot.InstallHostFDESaveDir), 0755), IsNil)

	// nothing was written yet
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"))), Equals, false)
	c.Check(device.HasEncryptedMarkerUnder(filepath.Join(d, boot.InstallHostFDESaveDir)), Equals, false)

	err := device.WriteEncryptionMarkers(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), filepath.Join(d, boot.InstallHostFDESaveDir), []byte("foo"))
	c.Assert(err, IsNil)
	// both markers were written
	c.Check(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "marker"), testutil.FileEquals, "foo")
	c.Check(filepath.Join(d, boot.InstallHostFDESaveDir, "marker"), testutil.FileEquals, "foo")
	// and can be read with device.ReadEncryptionMarkers
	m1, m2, err := device.ReadEncryptionMarkers(filepath.Join(d, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), filepath.Join(d, boot.InstallHostFDESaveDir))
	c.Assert(err, IsNil)
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
	err := os.MkdirAll(filepath.Dir(p1), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(p1, []byte("marker-p1"), 0600)
	c.Assert(err, IsNil)

	p2 := filepath.Join(tmpdir, boot.InstallHostFDESaveDir, "marker")
	err = os.MkdirAll(filepath.Dir(p2), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(p2, []byte("marker-p2"), 0600)
	c.Assert(err, IsNil)

	// reading them returns the two different values
	m1, m2, err := device.ReadEncryptionMarkers(filepath.Join(tmpdir, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")), filepath.Join(tmpdir, boot.InstallHostFDESaveDir))
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
		err := device.StampSealedKeys(root, tc.mth)
		c.Assert(err, IsNil)

		mth, err := device.SealedKeysMethod(root)
		c.Assert(err, IsNil)
		c.Check(tc.mth, Equals, mth)

		content, err := os.ReadFile(filepath.Join(root, "/var/lib/snapd/device/fde/sealed-keys"))
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, tc.expected)
	}
}

func (s *deviceSuite) TestSealedKeysMethodWithMissingStamp(c *C) {
	root := c.MkDir()

	_, err := device.SealedKeysMethod(root)
	c.Check(err, Equals, device.ErrNoSealedKeys)
}

func (s *deviceSuite) TestSealedKeysMethodWithWrongContentHappy(c *C) {
	root := c.MkDir()

	mockSealedKeyPath := filepath.Join(root, "/var/lib/snapd/device/fde/sealed-keys")
	err := os.MkdirAll(filepath.Dir(mockSealedKeyPath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSealedKeyPath, []byte("invalid-sealing-method"), 0600)
	c.Assert(err, IsNil)

	// invalid/unknown sealing methods do not error
	mth, err := device.SealedKeysMethod(root)
	c.Check(err, IsNil)
	c.Check(string(mth), Equals, "invalid-sealing-method")
}

func (s *deviceSuite) TestVolumesAuthOptionsValidateHappy(c *C) {
	var opts *device.VolumesAuthOptions

	// VolumesAuthOptions can be nil
	c.Assert(opts.Validate(), IsNil)
	// Valid kdf types
	for _, kdfType := range []string{"argon2i", "argon2id", "pbkdf2"} {
		opts = &device.VolumesAuthOptions{
			Mode:       device.AuthModePassphrase,
			Passphrase: "1234",
			KDFType:    kdfType,
			KDFTime:    2 * time.Second,
		}
		c.Assert(opts.Validate(), IsNil)
	}
	// KDF type and time are optional
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "1234"}
	c.Assert(opts.Validate(), IsNil)
	// Check PINs validation for good measure
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePIN, PIN: "1234"}
	c.Assert(opts.Validate(), IsNil)
}

func (s *deviceSuite) TestVolumesAuthOptionsValidateError(c *C) {
	// Bad auth mode
	opts := &device.VolumesAuthOptions{Mode: "bad-mode", Passphrase: "1234"}
	c.Assert(opts.Validate(), ErrorMatches, `invalid authentication mode "bad-mode", only "passphrase" and "pin" modes are supported`)
	// Empty passphrase
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePassphrase}
	c.Assert(opts.Validate(), ErrorMatches, "passphrase cannot be empty")
	// Empty PIN
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePIN}
	c.Assert(opts.Validate(), ErrorMatches, "PIN cannot be empty")
	// Long PIN
	var longPIN strings.Builder
	for i := 0; i < math.MaxUint8+1; i++ {
		longPIN.WriteString("0")
	}
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePIN, PIN: longPIN.String()}
	c.Assert(opts.Validate(), ErrorMatches, "PIN length cannot exceed 255")
	// Non-digit PIN
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePIN, PIN: "abc123"}
	c.Assert(opts.Validate(), ErrorMatches, "PIN can only contain base-10 digits")
	// PIN mode + custom kdf type
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePIN, KDFType: "argon2i"}
	c.Assert(opts.Validate(), ErrorMatches, `"pin" authentication mode does not support custom kdf types`)
	// Bad kdf type
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "1234", KDFType: "bad-type"}
	c.Assert(opts.Validate(), ErrorMatches, `invalid kdf type "bad-type", only "argon2i", "argon2id" and "pbkdf2" are supported`)
	// Negative kdf time
	opts = &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "1234", KDFTime: -1}
	c.Assert(opts.Validate(), ErrorMatches, "kdf time cannot be negative")
}

func (s *deviceSuite) TestValidatePassphraseOrPINEntropy(c *C) {
	var qualityErr *device.AuthQualityError

	err := device.ValidatePassphraseOrPINEntropy(device.AuthModePassphrase, "test")
	c.Assert(errors.As(err, &qualityErr), Equals, true)
	c.Assert(qualityErr.Entropy < qualityErr.MinEntropy, Equals, true)
	c.Assert(qualityErr.MinEntropy, Equals, float64(42))
	c.Assert(err, ErrorMatches, `calculated entropy .* is less than the required minimum entropy \(42.00\) for the "passphrase" authentication mode`)

	err = device.ValidatePassphraseOrPINEntropy(device.AuthModePassphrase, "this is a good password")
	c.Assert(err, IsNil)

	err = device.ValidatePassphraseOrPINEntropy(device.AuthModePIN, "1234")
	c.Assert(errors.As(err, &qualityErr), Equals, true)
	c.Assert(qualityErr.Entropy < qualityErr.MinEntropy, Equals, true)
	c.Assert(qualityErr.MinEntropy, Equals, float64(13.3))
	c.Assert(err, ErrorMatches, `calculated entropy .* is less than the required minimum entropy \(13.30\) for the "pin" authentication mode`)

	err = device.ValidatePassphraseOrPINEntropy(device.AuthModePIN, "20250123")
	c.Assert(err, IsNil)
}
