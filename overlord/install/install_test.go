// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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

package install_test

import (
	"bytes"
	"crypto"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestInstall(t *testing.T) { TestingT(t) }

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
	snapYaml        = seedtest.MergeSampleSnapYaml(seedtest.SampleSnapYaml, map[string]string{
		"pc-kernel=20-fde-setup": `name: pc-kernel
version: 1.0
type: kernel
hooks:
 fde-setup:
`,
	})
)

type installSuite struct {
	testutil.BaseTest

	*seedtest.TestingSeed20

	perfTimings timings.Measurer

	configureTargetSystemOptsPassed []*sysconfig.Options
	configureTargetSystemErr        error
}

var _ = Suite(&installSuite{})

func (s *installSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir(dirs.GlobalRootDir) })

	s.AddCleanup(osutil.MockMountInfo(``))

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	// needed by TestingSeed20.MakeSeed (to work with makeSnap)

	restore := release.MockOnClassic(false)
	defer restore()

	s.SeedDir = dirs.SnapSeedDir

	s.perfTimings = timings.New(nil)

	s.configureTargetSystemOptsPassed = nil
	s.configureTargetSystemErr = nil
	restore = install.MockSysconfigConfigureTargetSystem(func(mod *asserts.Model, opts *sysconfig.Options) error {
		c.Check(mod, NotNil)
		s.configureTargetSystemOptsPassed = append(s.configureTargetSystemOptsPassed, opts)
		return s.configureTargetSystemErr
	})
	s.AddCleanup(restore)
}

func (s *installSuite) makeSnap(c *C, yamlKey, publisher string) {
	if publisher == "" {
		publisher = "canonical"
	}
	s.MakeAssertedSnap(c, snapYaml[yamlKey], nil, snap.R(1), publisher, s.StoreSigning.Database)
}

// XXX share
var uc20gadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed
        type: 21686148-6449-6E6F-744E-656564454649
        size: 20M
      - name: ubuntu-boot
        role: system-boot
        type: 21686148-6449-6E6F-744E-656564454649
        size: 10M
      - name: ubuntu-data
        role: system-data
        type: 21686148-6449-6E6F-744E-656564454649
        size: 50M
`

var uc20gadgetYamlWithSave = uc20gadgetYaml + `
      - name: ubuntu-save
        role: system-save
        type: 21686148-6449-6E6F-744E-656564454649
        size: 50M
`

func unpackSnap(snapBlob, targetDir string) error {
	if out, err := exec.Command("unsquashfs", "-d", targetDir, "-f", snapBlob).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot unsquashfs: %v", osutil.OutputErr(out, err))
	}
	return nil
}

func (s *installSuite) kernelSnap(c *C, yamlKey string) *snap.Info {
	s.makeSnap(c, yamlKey, "")
	return s.AssertedSnapInfo("pc-kernel")
}

func (s *installSuite) mountedGadget(c *C) (gadgetInfo *gadget.Info, gadgetDir string) {
	files := [][]string{
		{"meta/gadget.yaml", uc20gadgetYamlWithSave},
	}
	s.MakeAssertedSnap(c, "name: pc\nversion: 1.0\ntype: gadget\nbase: core20", files, snap.R(1), "canonical", s.StoreSigning.Database)

	gadgetDir = c.MkDir()
	err := unpackSnap(s.AssertedSnap("pc"), gadgetDir)
	c.Assert(err, IsNil)

	gadgetInfo, err = gadget.ReadInfo(gadgetDir, nil)
	c.Assert(err, IsNil)
	return gadgetInfo, gadgetDir
}

func (s *installSuite) mockModel(override map[string]interface{}) *asserts.Model {
	m := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}}}
	for n, v := range override {
		m[n] = v
	}
	return s.Brands.Model("my-brand", "my-model", m)
}

func (s *installSuite) TestEncryptionSupportInfoWithTPM(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	gadgetInfo, _ := s.mountedGadget(c)

	var testCases = []struct {
		grade, storageSafety string
		tpmErr               error

		expected install.EncryptionSupportInfo
	}{
		{
			"dangerous", "", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"dangerous", "", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:      asserts.StorageSafetyPreferEncrypted,
				Type:               secboot.EncryptionTypeNone,
				UnavailableWarning: "not encrypting device storage as checking TPM gave: no tpm",
			},
		}, {
			"dangerous", "encrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"dangerous", "encrypted", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot encrypt device storage as mandated by encrypted storage-safety model option: no tpm"),
			},
		},
		{
			"dangerous", "prefer-unencrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferUnencrypted,
				// Note that encryption type is set to what is available
				Type: secboot.EncryptionTypeLUKS,
			},
		},
		{
			"signed", "", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"signed", "", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:      asserts.StorageSafetyPreferEncrypted,
				Type:               secboot.EncryptionTypeNone,
				UnavailableWarning: "not encrypting device storage as checking TPM gave: no tpm",
			},
		}, {
			"signed", "encrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"signed", "prefer-unencrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferUnencrypted,
				// Note that encryption type is set to what is available
				Type: secboot.EncryptionTypeLUKS,
			},
		}, {
			"signed", "encrypted", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot encrypt device storage as mandated by encrypted storage-safety model option: no tpm"),
			},
		}, {
			"secured", "encrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"secured", "encrypted", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot encrypt device storage as mandated by model grade secured: no tpm"),
			},
		}, {
			"secured", "", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"secured", "", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot encrypt device storage as mandated by model grade secured: no tpm"),
			},
		},
	}
	for _, tc := range testCases {
		restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return tc.tpmErr })
		defer restore()

		mockModel := s.mockModel(map[string]interface{}{
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
		})

		res, err := install.GetEncryptionSupportInfo(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, nil)
		c.Assert(err, IsNil)
		c.Check(res, DeepEquals, tc.expected, Commentf("%v", tc))
	}
}

func (s *installSuite) TestEncryptionSupportInfoForceUnencrypted(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	gadgetInfo, _ := s.mountedGadget(c)

	var testCases = []struct {
		grade, storageSafety, forceUnencrypted string
		tpmErr                                 error

		expected install.EncryptionSupportInfo
	}{
		{
			"dangerous", "", "", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		},
		{
			"dangerous", "", "force-unencrypted", nil,
			install.EncryptionSupportInfo{
				// Encryption is forcefully disabled
				// here so no further
				// availability/enc-type checks are
				// performed.
				Available: false, Disabled: true,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeNone,
			},
		},
		{
			"dangerous", "", "force-unencrypted", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				// Encryption is forcefully disabled
				// here so the "no tpm" error is never visible
				Available: false, Disabled: true,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeNone,
			},
		},
		// not possible to disable encryption on non-dangerous devices
		{
			"signed", "", "", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		},
		{
			"signed", "", "force-unencrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		},
		{
			"signed", "", "force-unencrypted", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:      asserts.StorageSafetyPreferEncrypted,
				Type:               secboot.EncryptionTypeNone,
				UnavailableWarning: "not encrypting device storage as checking TPM gave: no tpm",
			},
		},
		{
			"secured", "", "", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		},
		{
			"secured", "", "force-unencrypted", nil,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		},
		{
			"secured", "", "force-unencrypted", fmt.Errorf("no tpm"),
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot encrypt device storage as mandated by model grade secured: no tpm"),
			},
		},
	}

	for _, tc := range testCases {
		restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return tc.tpmErr })
		defer restore()

		mockModel := s.mockModel(map[string]interface{}{
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
		})
		forceUnencryptedPath := filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")
		if tc.forceUnencrypted == "" {
			os.Remove(forceUnencryptedPath)
		} else {
			err := os.MkdirAll(filepath.Dir(forceUnencryptedPath), 0755)
			c.Assert(err, IsNil)
			err = os.WriteFile(forceUnencryptedPath, nil, 0644)
			c.Assert(err, IsNil)
		}

		res, err := install.GetEncryptionSupportInfo(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, nil)
		c.Assert(err, IsNil)
		c.Check(res, DeepEquals, tc.expected, Commentf("%v", tc))
	}
}

var gadgetWithoutUbuntuSave = &gadget.Info{
	Volumes: map[string]*gadget.Volume{
		"pc": {
			Name:       "pc",
			Schema:     "mbr",
			Bootloader: "grub",
			Structure: []gadget.VolumeStructure{
				{VolumeName: "ubuntu-seed", Name: "ubuntu-seed", Label: "ubuntu-seed", Size: 700 * quantity.SizeMiB, Role: "system-seed", Filesystem: "vfat"},
				{VolumeName: "ubuntu-data", Name: "ubuntu-data", Label: "ubuntu-data", Size: 700 * quantity.SizeMiB, Role: "system-data", Filesystem: "ext4"},
			},
		},
	},
}

var gadgetUC20 = &gadget.Info{
	Volumes: map[string]*gadget.Volume{
		"pc": {
			Name:       "pc",
			Schema:     "mbr",
			Bootloader: "grub",
			Structure: []gadget.VolumeStructure{
				{VolumeName: "ubuntu-seed", Name: "ubuntu-seed", Label: "ubuntu-seed", Size: 700 * quantity.SizeMiB, Role: "system-seed", Filesystem: "vfat"},
				{VolumeName: "ubuntu-data", Name: "ubuntu-data", Label: "ubuntu-data", Size: 700 * quantity.SizeMiB, Role: "system-data", Filesystem: "ext4"},
				{VolumeName: "ubuntu-save", Name: "ubuntu-save", Label: "ubuntu-save", Size: 5 * quantity.SizeMiB, Role: "system-save", Filesystem: "ext4"},
			},
		},
	},
}

func (s *installSuite) TestEncryptionSupportInfoGadgetIncompatibleWithEncryption(c *C) {
	restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return nil })
	defer restore()

	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	var testCases = []struct {
		grade, storageSafety string
		gadgetInfo           *gadget.Info

		expected install.EncryptionSupportInfo
	}{
		{
			"dangerous", "", gadgetUC20,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"dangerous", "", gadgetWithoutUbuntuSave,
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:      asserts.StorageSafetyPreferEncrypted,
				Type:               secboot.EncryptionTypeNone,
				UnavailableWarning: "cannot use encryption with the gadget, disabling encryption: gadget does not support encrypted data: required partition with system-save role is missing",
			},
		}, {
			"dangerous", "encrypted", gadgetWithoutUbuntuSave,
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot use encryption with the gadget: gadget does not support encrypted data: required partition with system-save role is missing"),
			},
		}, {
			"signed", "", gadgetUC20,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyPreferEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"signed", "", gadgetWithoutUbuntuSave,
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:      asserts.StorageSafetyPreferEncrypted,
				Type:               secboot.EncryptionTypeNone,
				UnavailableWarning: "cannot use encryption with the gadget, disabling encryption: gadget does not support encrypted data: required partition with system-save role is missing",
			},
		}, {
			"signed", "encrypted", gadgetWithoutUbuntuSave,
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot use encryption with the gadget: gadget does not support encrypted data: required partition with system-save role is missing"),
			},
		}, {
			"secured", "", gadgetUC20,
			install.EncryptionSupportInfo{
				Available: true, Disabled: false,
				StorageSafety: asserts.StorageSafetyEncrypted,
				Type:          secboot.EncryptionTypeLUKS,
			},
		}, {
			"secured", "", gadgetWithoutUbuntuSave,
			install.EncryptionSupportInfo{
				Available: false, Disabled: false,
				StorageSafety:  asserts.StorageSafetyEncrypted,
				Type:           secboot.EncryptionTypeNone,
				UnavailableErr: fmt.Errorf("cannot use encryption with the gadget: gadget does not support encrypted data: required partition with system-save role is missing"),
			},
		},
	}
	for _, tc := range testCases {
		mockModel := s.mockModel(map[string]interface{}{
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
		})

		res, err := install.GetEncryptionSupportInfo(mockModel, secboot.TPMProvisionFull, kernelInfo, tc.gadgetInfo, nil)
		c.Assert(err, IsNil)
		c.Check(res, DeepEquals, tc.expected, Commentf("%v", tc))
	}
}

func (s *installSuite) TestInstallCheckEncryptedFDEHook(c *C) {
	for _, tc := range []struct {
		hookOutput  string
		expectedErr string

		encryptionType secboot.EncryptionType
	}{
		// invalid json
		{"xxx", `cannot parse hook output "xxx": invalid character 'x' looking for beginning of value`, secboot.EncryptionTypeNone},
		// no output is invalid
		{"", `cannot parse hook output "": unexpected end of JSON input`, secboot.EncryptionTypeNone},
		// specific error
		{`{"error":"failed"}`, `cannot use hook: it returned error: failed`, secboot.EncryptionTypeNone},
		{`{}`, `cannot use hook: neither "features" nor "error" returned`, secboot.EncryptionTypeNone},
		// valid
		{`{"features":[]}`, "", secboot.EncryptionTypeLUKS},
		{`{"features":["a"]}`, "", secboot.EncryptionTypeLUKS},
		{`{"features":["a","b"]}`, "", secboot.EncryptionTypeLUKS},
		// features must be list of strings
		{`{"features":[1]}`, `cannot parse hook output ".*": json: cannot unmarshal number into Go struct.*`, secboot.EncryptionTypeNone},
		{`{"features":1}`, `cannot parse hook output ".*": json: cannot unmarshal number into Go struct.*`, secboot.EncryptionTypeNone},
		{`{"features":"1"}`, `cannot parse hook output ".*": json: cannot unmarshal string into Go struct.*`, secboot.EncryptionTypeNone},
		// valid and uses ice
		{`{"features":["a","inline-crypto-engine","b"]}`, "", secboot.EncryptionTypeLUKSWithICE},
	} {
		runFDESetup := func(_ *fde.SetupRequest) ([]byte, error) {
			return []byte(tc.hookOutput), nil
		}

		et, err := install.CheckFDEFeatures(runFDESetup)
		if tc.expectedErr != "" {
			c.Check(err, ErrorMatches, tc.expectedErr, Commentf("%v", tc))
		} else {
			c.Check(err, IsNil, Commentf("%v", tc))
			c.Check(et, Equals, tc.encryptionType, Commentf("%v", tc))
		}
	}
}

func (s *installSuite) TestInstallCheckEncryptionSupportTPM(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	gadgetInfo, _ := s.mountedGadget(c)

	mockModel := s.mockModel(nil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	for _, tc := range []struct {
		hasTPM         bool
		encryptionType secboot.EncryptionType
	}{
		// unhappy: no tpm, no hook
		{false, secboot.EncryptionTypeNone},
		// happy: tpm
		{true, secboot.EncryptionTypeLUKS},
	} {
		restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error {
			if tc.hasTPM {
				return nil
			}
			return fmt.Errorf("tpm says no")
		})
		defer restore()

		encryptionType, err := install.CheckEncryptionSupport(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, nil)
		c.Assert(err, IsNil)
		c.Check(encryptionType, Equals, tc.encryptionType, Commentf("%v", tc))
		if !tc.hasTPM {
			c.Check(logbuf.String(), Matches, ".*: not encrypting device storage as checking TPM gave: tpm says no\n")
		}
		logbuf.Reset()
	}
}

func (s *installSuite) TestInstallCheckEncryptionSupportHook(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20-fde-setup")

	gadgetInfo, _ := s.mountedGadget(c)

	mockModel := s.mockModel(nil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	for _, tc := range []struct {
		fdeSetupHookFeatures string

		hasTPM         bool
		encryptionType secboot.EncryptionType
	}{
		{"[]", false, secboot.EncryptionTypeLUKS},
		{"[]", true, secboot.EncryptionTypeLUKS},
	} {
		runFDESetup := func(_ *fde.SetupRequest) ([]byte, error) {
			return []byte(fmt.Sprintf(`{"features":%s}`, tc.fdeSetupHookFeatures)), nil
		}

		restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error {
			if tc.hasTPM {
				return nil
			}
			return fmt.Errorf("tpm says no")
		})
		defer restore()

		encryptionType, err := install.CheckEncryptionSupport(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, runFDESetup)
		c.Assert(err, IsNil)
		c.Check(encryptionType, Equals, tc.encryptionType, Commentf("%v", tc))
		if !tc.hasTPM {
			c.Check(logbuf.String(), Equals, "")
		}
		logbuf.Reset()
	}
}

func (s *installSuite) TestInstallCheckEncryptionSupportStorageSafety(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	gadgetInfo, _ := s.mountedGadget(c)

	restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return nil })
	defer restore()

	var testCases = []struct {
		grade, storageSafety string

		expectedEncryption bool
	}{
		// we don't test unset here because the assertion assembly
		// will ensure it has a default
		{"dangerous", "prefer-unencrypted", false},
		{"dangerous", "prefer-encrypted", true},
		{"dangerous", "encrypted", true},
		{"signed", "prefer-unencrypted", false},
		{"signed", "prefer-encrypted", true},
		{"signed", "encrypted", true},
		// secured+prefer-{,un}encrypted is an error at the
		// assertion level already so cannot be tested here
		{"secured", "encrypted", true},
	}
	for _, tc := range testCases {
		mockModel := s.mockModel(map[string]interface{}{
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
		})

		encryptionType, err := install.CheckEncryptionSupport(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, nil)
		c.Assert(err, IsNil)
		encrypt := (encryptionType != secboot.EncryptionTypeNone)
		c.Check(encrypt, Equals, tc.expectedEncryption, Commentf("%v", tc))
	}
}

func (s *installSuite) TestInstallCheckEncryptionSupportErrors(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	gadgetInfo, _ := s.mountedGadget(c)

	restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return fmt.Errorf("tpm says no") })
	defer restore()

	var testCases = []struct {
		grade, storageSafety string

		expectedErr string
	}{
		// we don't test unset here because the assertion assembly
		// will ensure it has a default
		{
			"dangerous", "encrypted",
			"cannot encrypt device storage as mandated by encrypted storage-safety model option: tpm says no",
		}, {
			"signed", "encrypted",
			"cannot encrypt device storage as mandated by encrypted storage-safety model option: tpm says no",
		}, {
			"secured", "",
			"cannot encrypt device storage as mandated by model grade secured: tpm says no",
		}, {
			"secured", "encrypted",
			"cannot encrypt device storage as mandated by model grade secured: tpm says no",
		},
	}
	for _, tc := range testCases {
		mockModel := s.mockModel(map[string]interface{}{
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
		})

		_, err := install.CheckEncryptionSupport(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, nil)
		c.Check(err, ErrorMatches, tc.expectedErr, Commentf("%s %s", tc.grade, tc.storageSafety))
	}
}

func (s *installSuite) TestInstallCheckEncryptionSupportErrorsLogsTPM(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20")

	gadgetInfo, _ := s.mountedGadget(c)

	restore := install.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error {
		return fmt.Errorf("tpm says no")
	})
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	mockModel := s.mockModel(nil)

	_, err := install.CheckEncryptionSupport(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, nil)
	c.Check(err, IsNil)
	c.Check(logbuf.String(), Matches, "(?s).*: not encrypting device storage as checking TPM gave: tpm says no\n")
}

func (s *installSuite) TestInstallCheckEncryptionSupportErrorsLogsHook(c *C) {
	kernelInfo := s.kernelSnap(c, "pc-kernel=20-fde-setup")

	gadgetInfo, _ := s.mountedGadget(c)

	runFDESetup := func(_ *fde.SetupRequest) ([]byte, error) {
		return nil, fmt.Errorf("hook error")
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	mockModel := s.mockModel(nil)

	_, err := install.CheckEncryptionSupport(mockModel, secboot.TPMProvisionFull, kernelInfo, gadgetInfo, runFDESetup)
	c.Check(err, IsNil)
	c.Check(logbuf.String(), Matches, "(?s).*: not encrypting device storage as querying kernel fde-setup hook did not succeed:.*\n")
}

func (s *installSuite) mockBootloader(c *C, trustedAssets bool, managedAssets bool) {
	bootloaderRootdir := c.MkDir()

	if trustedAssets || managedAssets {
		tab := bootloadertest.Mock("trusted", bootloaderRootdir).WithTrustedAssets()
		if trustedAssets {
			tab.TrustedAssetsMap = map[string]string{"trusted-asset": "trusted-asset"}
		}
		if managedAssets {
			tab.ManagedAssetsList = []string{"managed-asset"}
		}
		bootloader.Force(tab)
		s.AddCleanup(func() { bootloader.Force(nil) })

		err := os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "trusted-asset"), nil, 0644)
		c.Assert(err, IsNil)
	} else {
		bl := bootloadertest.Mock("mock", bootloaderRootdir)
		bootloader.Force(bl)
	}

	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *installSuite) TestBuildInstallObserver(c *C) {
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	cases := []struct {
		trustedAssets bool
		managedAssets bool
		useEncryption bool
		observer      bool
	}{
		{trustedAssets: true, useEncryption: true, observer: true},
		{trustedAssets: true, useEncryption: false, observer: false},
		{trustedAssets: false, managedAssets: true, useEncryption: true, observer: true},
		{trustedAssets: false, managedAssets: true, useEncryption: false, observer: true},
		{trustedAssets: false, useEncryption: true, observer: true},
		{trustedAssets: false, useEncryption: false, observer: false},
	}

	for _, tc := range cases {
		s.mockBootloader(c, tc.trustedAssets, tc.managedAssets)

		co, to, err := install.BuildInstallObserver(mockModel, gadgetDir, tc.useEncryption)
		c.Assert(err, IsNil)
		tcComm := Commentf("%#v", tc)
		if tc.observer {
			c.Check(co, NotNil, tcComm)
			if tc.useEncryption {
				c.Check(to == co, Equals, true, tcComm)
			} else {
				c.Check(to, IsNil, tcComm)
			}
		} else {
			c.Check(co, testutil.IsInterfaceNil, tcComm)
			c.Check(to, IsNil, tcComm)
		}

	}
}

var (
	dataEncryptionKey = keys.EncryptionKey{'d', 'a', 't', 'a', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	saveKey           = keys.EncryptionKey{'s', 'a', 'v', 'e', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
)

func (s *installSuite) TestPrepareEncryptedSystemData(c *C) {
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	trustedAssets := true
	s.mockBootloader(c, trustedAssets, false)

	useEncryption := true
	_, to, err := install.BuildInstallObserver(mockModel, gadgetDir, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(to, NotNil)

	// We are required to call ObserveExistingTrustedRecoveryAssets on trusted observers
	err = to.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir)
	c.Assert(err, IsNil)

	keyForRole := map[string]keys.EncryptionKey{
		gadget.SystemData: dataEncryptionKey,
		gadget.SystemSave: saveKey,
	}
	err = install.PrepareEncryptedSystemData(mockModel, keyForRole, to)
	c.Assert(err, IsNil)

	c.Check(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "ubuntu-save.key"), testutil.FileEquals, []byte(saveKey))
	marker, err := os.ReadFile(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "marker"))
	c.Assert(err, IsNil)
	c.Check(marker, HasLen, 32)
	c.Check(filepath.Join(boot.InstallHostFDESaveDir, "marker"), testutil.FileEquals, marker)

	// the assets cache was written to
	l, err := os.ReadDir(filepath.Join(dirs.SnapBootAssetsDir, "trusted"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
}

func (s *installSuite) TestPrepareRunSystemDataWritesModel(c *C) {
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	err := install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	var buf bytes.Buffer
	err = asserts.NewEncoder(&buf).Encode(mockModel)
	c.Assert(err, IsNil)

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileEquals, buf.String())
}

func (s *installSuite) TestPrepareRunSystemDataRunsSysconfig(c *C) {
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	err := install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	// and sysconfig.ConfigureTargetSystem was run exactly once
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:      gadgetDir,
		},
	})

	// and the special dirs in _writable_defaults were created
	for _, dir := range []string{"/etc/udev/rules.d/", "/etc/modules-load.d/", "/etc/modprobe.d/"} {
		fullDir := filepath.Join(sysconfig.WritableDefaultsDir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), dir)
		c.Assert(fullDir, testutil.FilePresent)
	}
}

func (s *installSuite) TestPrepareRunSystemDataRunSysconfigErr(c *C) {
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	s.configureTargetSystemErr = fmt.Errorf("error from sysconfig.ConfigureTargetSystem")

	err := install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Check(err, ErrorMatches, `error from sysconfig.ConfigureTargetSystem`)
	// and sysconfig.ConfigureTargetSystem was run exactly once
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:      gadgetDir,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataSupportsCloudInitInDangerous(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = os.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	err = install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	// and did tell sysconfig about the cloud-init files
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  true,
			CloudInitSrcDir: filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d"),
			TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:       gadgetDir,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataSupportsCloudInitGadgetAndSeedConfigSigned(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = os.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(map[string]interface{}{
		"grade": "signed",
	})

	// we also have gadget cloud init too
	err = os.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), nil, 0644)
	c.Assert(err, IsNil)

	err = install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	// sysconfig is told about both configs
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  true,
			TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:       gadgetDir,
			CloudInitSrcDir: cloudCfg,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataSupportsCloudInitBothGadgetAndUbuntuSeedDangerous(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = os.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	// we also have gadget cloud init too
	err = os.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), nil, 0644)
	c.Assert(err, IsNil)

	err = install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	// and did tell sysconfig about the cloud-init files
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  true,
			CloudInitSrcDir: filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d"),
			TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:       gadgetDir,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataSignedNoUbuntuSeedCloudInit(c *C) {
	// pretend we have no cloud-init config anywhere
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(map[string]interface{}{
		"grade": "signed",
	})

	err := install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	// we didn't pass any cloud-init src dir but still left cloud-init enabled
	// if for example a CI-DATA USB drive was provided at runtime
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:      gadgetDir,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataSecuredGadgetCloudConfCloudInit(c *C) {
	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(map[string]interface{}{
		"grade": "secured",
	})

	// pretend we have a cloud.conf from the gadget
	err := os.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), nil, 0644)
	c.Assert(err, IsNil)

	err = install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:      gadgetDir,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataSecuredNoUbuntuSeedCloudInit(c *C) {
	// pretend we have a cloud-init config on the seed partition with some files
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = os.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(map[string]interface{}{
		"grade": "secured",
	})

	err = install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	// we did tell sysconfig about the ubuntu-seed cloud config dir because it
	// exists, but it is up to sysconfig to use the model to determine to ignore
	// the files
	c.Assert(s.configureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  false,
			TargetRootDir:   filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"),
			GadgetDir:       gadgetDir,
			CloudInitSrcDir: cloudCfg,
		},
	})
}

func (s *installSuite) TestPrepareRunSystemDataWritesTimesyncdClockHappy(c *C) {
	now := time.Now()
	restore := install.MockTimeNow(func() time.Time { return now })
	defer restore()

	clockTsInSrc := filepath.Join(dirs.GlobalRootDir, "/var/lib/systemd/timesync/clock")
	c.Assert(os.MkdirAll(filepath.Dir(clockTsInSrc), 0755), IsNil)
	c.Assert(os.WriteFile(clockTsInSrc, nil, 0644), IsNil)
	// a month old timestamp file
	c.Assert(os.Chtimes(clockTsInSrc, now.AddDate(0, -1, 0), now.AddDate(0, -1, 0)), IsNil)

	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	err := install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Assert(err, IsNil)

	clockTsInDst := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "/var/lib/systemd/timesync/clock")
	fi, err := os.Stat(clockTsInDst)
	c.Assert(err, IsNil)
	c.Check(fi.ModTime().Round(time.Second), Equals, now.Round(time.Second))
	c.Check(fi.Size(), Equals, int64(0))
}

func (s *installSuite) TestPrepareRunSystemDataWritesTimesyncdClockErr(c *C) {
	now := time.Now()
	restore := install.MockTimeNow(func() time.Time { return now })
	defer restore()

	if os.Geteuid() == 0 {
		c.Skip("the test cannot be executed by the root user")
	}

	clockTsInSrc := filepath.Join(dirs.GlobalRootDir, "/var/lib/systemd/timesync/clock")
	c.Assert(os.MkdirAll(filepath.Dir(clockTsInSrc), 0755), IsNil)
	c.Assert(os.WriteFile(clockTsInSrc, nil, 0644), IsNil)

	timesyncDirInDst := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "/var/lib/systemd/timesync/")
	c.Assert(os.MkdirAll(timesyncDirInDst, 0755), IsNil)
	c.Assert(os.Chmod(timesyncDirInDst, 0000), IsNil)
	defer os.Chmod(timesyncDirInDst, 0755)

	_, gadgetDir := s.mountedGadget(c)
	mockModel := s.mockModel(nil)

	err := install.PrepareRunSystemData(mockModel, gadgetDir, s.perfTimings)
	c.Check(err, ErrorMatches, `cannot seed timesyncd clock: cannot copy clock:.*Permission denied.*`)
}

func (s *installSuite) setupCore20Seed(c *C) *asserts.Model {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "core20", "")
	s.mountedGadget(c)
	optSnapPath := snaptest.MakeTestSnapWithFiles(c, seedtest.SampleSnapYaml["optional20-a"], nil)

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core20",
				"id":   s.AssertedSnapID("core20"),
				"type": "base",
			}},
	}

	return s.MakeSeed(c, "20220401", "my-brand", "my-model", model, []*seedwriter.OptionsSnap{{Path: optSnapPath}})
}

func (s *installSuite) mockPreseedAssertion(c *C, brandID, modelName, series, preseedAsPath, sysLabel string, digest string, snaps []interface{}) {
	headers := map[string]interface{}{
		"type":              "preseed",
		"authority-id":      brandID,
		"series":            series,
		"brand-id":          brandID,
		"model":             modelName,
		"system-label":      sysLabel,
		"artifact-sha3-384": digest,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"snaps":             snaps,
	}

	signer := s.Brands.Signing(brandID)
	preseedAs, err := signer.Sign(asserts.PreseedType, headers, nil, "")
	if err != nil {
		panic(err)
	}

	f, err := os.Create(preseedAsPath)
	defer f.Close()
	c.Assert(err, IsNil)
	enc := asserts.NewEncoder(f)
	c.Assert(enc.Encode(preseedAs), IsNil)
}

func (s *installSuite) TestApplyPreseededData(c *C) {
	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	ubuntuSeedDir := dirs.SnapSeedDir
	sysLabel := "20220401"
	writableDir := filepath.Join(c.MkDir(), "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")

	restore := seed.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.setupCore20Seed(c)

	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)

	snaps := []interface{}{
		map[string]interface{}{"name": "snapd", "id": s.AssertedSnapID("snapd"), "revision": "1"},
		map[string]interface{}{"name": "core20", "id": s.AssertedSnapID("core20"), "revision": "1"},
		map[string]interface{}{"name": "pc-kernel", "id": s.AssertedSnapID("pc-kernel"), "revision": "1"},
		map[string]interface{}{"name": "pc", "id": s.AssertedSnapID("pc"), "revision": "1"},
		map[string]interface{}{"name": "optional20-a"},
	}
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, digest, snaps)

	// set a specific mod time on one of the snaps to verify it's preserved when the blob gets copied.
	pastTime, err := time.Parse(time.RFC3339, "2020-01-01T10:00:00Z")
	c.Assert(err, IsNil)
	c.Assert(os.Chtimes(filepath.Join(ubuntuSeedDir, "snaps", "snapd_1.snap"), pastTime, pastTime), IsNil)

	sysSeed, err := seed.Open(ubuntuSeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = sysSeed.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)
	preseedSeed := sysSeed.(seed.PreseedCapable)
	c.Check(preseedSeed.HasArtifact("preseed.tgz"), Equals, true)

	// restore root dir, otherwise paths referencing GlobalRootDir, such as from placeInfo.MountFile() get confused
	// in the test.
	dirs.SetRootDir("/")
	err = install.ApplyPreseededData(preseedSeed, writableDir)
	c.Assert(err, IsNil)

	c.Check(mockTarCmd.Calls(), DeepEquals, [][]string{
		{"tar", "--extract", "--preserve-permissions", "--preserve-order", "--gunzip", "--directory", writableDir, "-f", preseedArtifact},
	})

	for _, seedSnap := range []struct {
		name string
		blob string
	}{
		{"snapd/1", "snapd_1.snap"},
		{"core20/1", "core20_1.snap"},
		{"pc-kernel/1", "pc-kernel_1.snap"},
		{"pc/1", "pc_1.snap"},
		{"optional20-a/x1", "optional20-a_x1.snap"},
	} {
		c.Assert(osutil.FileExists(filepath.Join(writableDir, dirs.StripRootDir(dirs.SnapMountDir), seedSnap.name)), Equals, true, &dumpDirContents{c, writableDir})
		c.Assert(osutil.FileExists(filepath.Join(writableDir, dirs.SnapBlobDir, seedSnap.blob)), Equals, true, &dumpDirContents{c, writableDir})
	}

	// verify that modtime of the copied snap blob was preserved
	finfo, err := os.Stat(filepath.Join(writableDir, dirs.SnapBlobDir, "snapd_1.snap"))
	c.Assert(err, IsNil)
	c.Check(finfo.ModTime().Equal(pastTime), Equals, true)
}

type dumpDirContents struct {
	c   *C
	dir string
}

func (d *dumpDirContents) CheckCommentString() string {
	cmd := exec.Command("find", d.dir)
	data, err := cmd.CombinedOutput()
	d.c.Assert(err, IsNil)
	return fmt.Sprintf("writable dir contents:\n%s", data)
}

func (s *installSuite) TestApplyPreseededDataAssertionMissing(c *C) {
	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	ubuntuSeedDir := dirs.SnapSeedDir
	sysLabel := "20220401"
	writableDir := filepath.Join(c.MkDir(), "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")

	restore := seed.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	s.setupCore20Seed(c)

	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)

	sysSeed, err := seed.Open(ubuntuSeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = sysSeed.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)
	preseedSeed := sysSeed.(seed.PreseedCapable)
	c.Check(preseedSeed.HasArtifact("preseed.tgz"), Equals, true)

	err = install.ApplyPreseededData(preseedSeed, writableDir)
	c.Assert(err, ErrorMatches, `no seed preseed assertion`)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	// empty "preseed" assertion file
	c.Assert(os.WriteFile(preseedAsPath, nil, 0644), IsNil)

	err = install.ApplyPreseededData(preseedSeed, writableDir)
	c.Assert(err, ErrorMatches, `system preseed assertion file must contain a preseed assertion`)
}

func (s *installSuite) TestApplyPreseededDataSnapMismatch(c *C) {
	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	snapPath2 := filepath.Join(dirs.GlobalRootDir, "mode-snap_3.snap")
	c.Assert(os.WriteFile(snapPath1, nil, 0644), IsNil)
	c.Assert(os.WriteFile(snapPath2, nil, 0644), IsNil)

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)

	model := s.mockModel(map[string]interface{}{
		"grade": "dangerous",
	})

	sysSeed := &fakeSeed{
		model:           model,
		preseedArtifact: true,
		sysDir:          filepath.Join(ubuntuSeedDir, "systems", sysLabel),
		essentialSnaps:  []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1), SnapID: "id111111111111111111111111111111"}}},
		modeSnaps: []*seed.Snap{{Path: snapPath2, SideInfo: &snap.SideInfo{RealName: "mode-snap", Revision: snap.R(3), SnapID: "id222222222222222222222222222222"}},
			{Path: snapPath2, SideInfo: &snap.SideInfo{RealName: "mode-snap2"}}},
	}

	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")

	for _, tc := range []struct {
		snapName string
		rev      string
		snapID   string
		err      string
	}{
		{"essential-snap", "2", "id111111111111111111111111111111", `snap "essential-snap" has wrong revision 1 \(expected: 2\)`},
		{"essential-snap", "1", "id000000000000000000000000000000", `snap "essential-snap" has wrong snap id "id111111111111111111111111111111" \(expected: "id000000000000000000000000000000"\)`},
		{"mode-snap", "4", "id222222222222222222222222222222", `snap "mode-snap" has wrong revision 3 \(expected: 4\)`},
		{"mode-snap", "3", "id000000000000000000000000000000", `snap "mode-snap" has wrong snap id "id222222222222222222222222222222" \(expected: "id000000000000000000000000000000"\)`},
		{"mode-snap2", "3", "id000000000000000000000000000000", `snap "mode-snap2" has wrong revision unset \(expected: 3\)`},
		{"extra-snap", "1", "id000000000000000000000000000000", `seed has 3 snaps but 4 snaps are required by preseed assertion`},
	} {

		preseedAsSnaps := []interface{}{
			map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
			map[string]interface{}{"name": "mode-snap", "id": "id222222222222222222222222222222", "revision": "3"},
			map[string]interface{}{"name": "mode-snap2"},
		}

		var found bool
		for i, ps := range preseedAsSnaps {
			if ps.(map[string]interface{})["name"] == tc.snapName {
				preseedAsSnaps[i] = map[string]interface{}{"name": tc.snapName, "id": tc.snapID, "revision": tc.rev}
				found = true
				break
			}
		}
		if !found {
			preseedAsSnaps = append(preseedAsSnaps, map[string]interface{}{"name": tc.snapName, "id": tc.snapID, "revision": tc.rev})
		}

		s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, digest, preseedAsSnaps)
		err = install.ApplyPreseededData(sysSeed, writableDir)
		c.Assert(err, ErrorMatches, tc.err)
	}

	// mode-snap is presend in the seed but missing in the preseed assertion; add other-snap to preseed assertion
	// to satisfy the check for number of snaps.
	preseedAsSnaps := []interface{}{
		map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
		map[string]interface{}{"name": "other-snap", "id": "id333222222222222222222222222222", "revision": "2"},
		map[string]interface{}{"name": "mode-snap2"},
	}
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, digest, preseedAsSnaps)
	err = install.ApplyPreseededData(sysSeed, writableDir)
	c.Assert(err, ErrorMatches, `snap "mode-snap" not present in the preseed assertion`)
}

func (s *installSuite) TestApplyPreseededDataWrongDigest(c *C) {
	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	c.Assert(os.WriteFile(snapPath1, nil, 0644), IsNil)

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)

	model := s.mockModel(map[string]interface{}{
		"grade": "dangerous",
	})

	sysSeed := &fakeSeed{
		model:           model,
		preseedArtifact: true,
		sysDir:          filepath.Join(ubuntuSeedDir, "systems", sysLabel),
		essentialSnaps:  []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1)}}},
	}

	snaps := []interface{}{
		map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
	}

	wrongDigest := "DGOnW4ReT30BEH2FLkwkhcUaUKqqlPxhmV5xu-6YOirDcTgxJkrbR_traaaY1fAE"
	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, wrongDigest, snaps)

	err := install.ApplyPreseededData(sysSeed, writableDir)
	c.Assert(err, ErrorMatches, `invalid preseed artifact digest`)
}

type fakeSeed struct {
	sysDir          string
	modeSnaps       []*seed.Snap
	essentialSnaps  []*seed.Snap
	model           *asserts.Model
	preseedArtifact bool
}

func (fs *fakeSeed) ArtifactPath(relName string) string {
	return filepath.Join(fs.sysDir, relName)
}

func (fs *fakeSeed) HasArtifact(relName string) bool {
	return fs.preseedArtifact && relName == "preseed.tgz"
}

func (*fakeSeed) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	return nil
}

func (fs *fakeSeed) LoadPreseedAssertion() (*asserts.Preseed, error) {
	f, err := os.Open(filepath.Join(fs.sysDir, "preseed"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a, err := asserts.NewDecoder(f).Decode()
	if err != nil {
		return nil, err
	}
	return a.(*asserts.Preseed), nil
}

func (fs *fakeSeed) Model() *asserts.Model {
	return fs.model
}

func (*fakeSeed) Brand() (*asserts.Account, error) {
	return nil, nil
}

func (*fakeSeed) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	return nil
}

func (*fakeSeed) LoadEssentialMetaWithSnapHandler([]snap.Type, seed.ContainerHandler, timings.Measurer) error {
	return nil
}

func (*fakeSeed) LoadMeta(string, seed.ContainerHandler, timings.Measurer) error {
	return nil
}

func (*fakeSeed) UsesSnapdSnap() bool {
	return true
}

func (*fakeSeed) SetParallelism(n int) {}

func (fs *fakeSeed) EssentialSnaps() []*seed.Snap {
	return fs.essentialSnaps
}

func (fs *fakeSeed) ModeSnaps(mode string) ([]*seed.Snap, error) {
	return fs.modeSnaps, nil
}

func (*fakeSeed) NumSnaps() int {
	return 0
}

func (*fakeSeed) Iter(func(sn *seed.Snap) error) error {
	return nil
}
