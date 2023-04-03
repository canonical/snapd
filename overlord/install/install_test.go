// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
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
}

var _ = Suite(&installSuite{})

func (s *installSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir(dirs.GlobalRootDir) })

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	// needed by TestingSeed20.MakeSeed (to work with makeSnap)

	s.SeedDir = c.MkDir()
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
	s.MakeAssertedSnap(c, "name: pc\nversion: 1.0\ntype: gadget", files, snap.R(1), "canonical", s.StoreSigning.Database)

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
			err = ioutil.WriteFile(forceUnencryptedPath, nil, 0644)
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
