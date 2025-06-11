// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2021 Canonical Ltd
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

package secboot_test

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/linux"
	"github.com/canonical/go-tpm2/mu"
	sb "github.com/snapcore/secboot"
	sb_efi "github.com/snapcore/secboot/efi"
	sb_hooks "github.com/snapcore/secboot/hooks"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

type secbootSuite struct {
	testutil.BaseTest

	currentModel sb.SnapModel
}

var _ = Suite(&secbootSuite{})

func (s *secbootSuite) SetUpTest(c *C) {
	rootDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(rootDir, "/run"), 0755)
	c.Assert(err, IsNil)
	dirs.SetRootDir(rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.AddCleanup(secboot.MockSbSetModel(func(model sb.SnapModel) {
		s.currentModel = model
	}))
	s.AddCleanup(secboot.MockSbSetBootMode(func(mode string) {
	}))
	s.AddCleanup(secboot.MockSbSetKeyRevealer(func(kr sb_hooks.KeyRevealer) {
	}))
}

func (s *secbootSuite) TestCheckTPMKeySealingSupported(c *C) {
	c.Check(secboot.WithSecbootSupport, Equals, true)

	sbEmpty := []uint8{}
	sbEnabled := []uint8{1}
	sbDisabled := []uint8{0}
	efiNotSupported := []uint8(nil)
	tpmErr := errors.New("TPM error")

	tpmModesBoth := []secboot.TPMProvisionMode{secboot.TPMProvisionFull, secboot.TPMPartialReprovision}
	type testCase struct {
		tpmErr     error
		tpmEnabled bool
		tpmLockout bool
		tpmModes   []secboot.TPMProvisionMode
		sbData     []uint8
		err        string
	}
	for i, tc := range []testCase{
		// happy case
		{tpmErr: nil, tpmEnabled: true, tpmLockout: false, tpmModes: tpmModesBoth, sbData: sbEnabled, err: ""},
		// secure boot EFI var is empty
		{tpmErr: nil, tpmEnabled: true, tpmLockout: false, tpmModes: tpmModesBoth, sbData: sbEmpty, err: "secure boot variable does not exist"},
		// secure boot is disabled
		{tpmErr: nil, tpmEnabled: true, tpmLockout: false, tpmModes: tpmModesBoth, sbData: sbDisabled, err: "secure boot is disabled"},
		// EFI not supported
		{tpmErr: nil, tpmEnabled: true, tpmLockout: false, tpmModes: tpmModesBoth, sbData: efiNotSupported, err: "not a supported EFI system"},
		// TPM connection error
		{tpmErr: tpmErr, sbData: sbEnabled, tpmLockout: false, tpmModes: tpmModesBoth, err: "cannot connect to TPM device: TPM error"},
		// TPM was detected but it's not enabled
		{tpmErr: nil, tpmEnabled: false, tpmLockout: false, tpmModes: tpmModesBoth, sbData: sbEnabled, err: "TPM device is not enabled"},
		// No TPM device
		{tpmErr: sb_tpm2.ErrNoTPM2Device, tpmLockout: false, tpmModes: tpmModesBoth, sbData: sbEnabled, err: "cannot connect to TPM device: no TPM2 device is available"},

		// In DA lockout mode full provision errors
		{tpmErr: nil, tpmEnabled: true, tpmLockout: true, tpmModes: []secboot.TPMProvisionMode{secboot.TPMProvisionFull}, sbData: sbEnabled, err: "the TPM is in DA lockout mode"},

		// In DA lockout mode partial provision is fine
		{tpmErr: nil, tpmEnabled: true, tpmLockout: true, tpmModes: []secboot.TPMProvisionMode{secboot.TPMPartialReprovision}, sbData: sbEnabled, err: ""},
	} {
		c.Logf("%d: %v %v %v %v %q", i, tc.tpmErr, tc.tpmEnabled, tc.tpmModes, tc.sbData, tc.err)

		_, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		restore = secboot.MockSbLockoutAuthSet(func(tpm *sb_tpm2.Connection) bool {
			return tc.tpmLockout
		})
		defer restore()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		var vars map[string][]byte
		if tc.sbData != nil {
			vars = map[string][]byte{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c": tc.sbData}
		}
		restoreEfiVars := efi.MockVars(vars, nil)
		defer restoreEfiVars()

		for _, tpmMode := range tc.tpmModes {
			err := secboot.CheckTPMKeySealingSupported(tpmMode)
			if tc.err == "" {
				c.Assert(err, IsNil)
			} else {
				c.Assert(err, ErrorMatches, tc.err)
			}
		}
	}
}

func (s *secbootSuite) TestMeasureSnapSystemEpochWhenPossible(c *C) {
	for _, tc := range []struct {
		tpmErr     error
		tpmEnabled bool
		callNum    int
		err        string
	}{
		{
			// normal connection to the TPM device
			tpmErr: nil, tpmEnabled: true, callNum: 1, err: "",
		},
		{
			// TPM device exists but returns error
			tpmErr: errors.New("tpm error"), callNum: 0,
			err: "cannot measure snap system epoch: cannot open TPM connection: tpm error",
		},
		{
			// TPM device exists but is disabled
			tpmErr: nil, tpmEnabled: false,
		},
		{
			// TPM device does not exist
			tpmErr: sb_tpm2.ErrNoTPM2Device,
		},
	} {
		mockTpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		calls := 0
		restore = secboot.MockSbMeasureSnapSystemEpochToTPM(func(tpm *sb_tpm2.Connection, pcrIndex int) error {
			calls++
			c.Assert(tpm, Equals, mockTpm)
			c.Assert(pcrIndex, Equals, 12)
			return nil
		})
		defer restore()

		err := secboot.MeasureSnapSystemEpochWhenPossible()
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
		c.Assert(calls, Equals, tc.callNum)
	}
}

func (s *secbootSuite) TestMeasureSnapModelWhenPossible(c *C) {
	for i, tc := range []struct {
		tpmErr     error
		tpmEnabled bool
		modelErr   error
		callNum    int
		err        string
	}{
		{
			// normal connection to the TPM device
			tpmErr: nil, tpmEnabled: true, modelErr: nil, callNum: 1, err: "",
		},
		{
			// normal connection to the TPM device with model error
			tpmErr: nil, tpmEnabled: true, modelErr: errors.New("model error"), callNum: 0,
			err: "cannot measure snap model: model error",
		},
		{
			// TPM device exists but returns error
			tpmErr: errors.New("tpm error"), callNum: 0,
			err: "cannot measure snap model: cannot open TPM connection: tpm error",
		},
		{
			// TPM device exists but is disabled
			tpmErr: nil, tpmEnabled: false,
		},
		{
			// TPM device does not exist
			tpmErr: sb_tpm2.ErrNoTPM2Device,
		},
	} {
		c.Logf("%d: tpmErr:%v tpmEnabled:%v", i, tc.tpmErr, tc.tpmEnabled)
		mockModel := &asserts.Model{}

		mockTpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		calls := 0
		restore = secboot.MockSbMeasureSnapModelToTPM(func(tpm *sb_tpm2.Connection, pcrIndex int, model sb.SnapModel) error {
			calls++
			c.Assert(tpm, Equals, mockTpm)
			c.Assert(model, Equals, mockModel)
			c.Assert(pcrIndex, Equals, 12)
			return nil
		})
		defer restore()

		findModel := func() (*asserts.Model, error) {
			if tc.modelErr != nil {
				return nil, tc.modelErr
			}
			return mockModel, nil
		}

		err := secboot.MeasureSnapModelWhenPossible(findModel)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
		c.Assert(calls, Equals, tc.callNum)
	}
}

func (s *secbootSuite) TestLockTPMSealedKeys(c *C) {
	tt := []struct {
		tpmErr     error
		tpmEnabled bool
		lockOk     bool
		expError   string
	}{
		// can't connect to tpm
		{
			tpmErr:   fmt.Errorf("failed to connect to tpm"),
			expError: "cannot lock TPM: failed to connect to tpm",
		},
		// no TPM2 device, shouldn't return an error
		{
			tpmErr: sb_tpm2.ErrNoTPM2Device,
		},
		// tpm is not enabled but we can lock it
		{
			tpmEnabled: false,
			lockOk:     true,
		},
		// can't lock pcr protection profile
		{
			tpmEnabled: true,
			lockOk:     false,
			expError:   "block failed",
		},
		// tpm enabled, we can lock it
		{
			tpmEnabled: true,
			lockOk:     true,
		},
	}

	for _, tc := range tt {
		mockSbTPM, restoreConnect := mockSbTPMConnection(c, tc.tpmErr)
		defer restoreConnect()

		restore := secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		sbBlockPCRProtectionPolicesCalls := 0
		restore = secboot.MockSbBlockPCRProtectionPolicies(func(tpm *sb_tpm2.Connection, pcrs []int) error {
			sbBlockPCRProtectionPolicesCalls++
			c.Assert(tpm, Equals, mockSbTPM)
			c.Assert(pcrs, DeepEquals, []int{12})
			if tc.lockOk {
				return nil
			}
			return errors.New("block failed")
		})
		defer restore()

		err := secboot.LockTPMSealedKeys()
		if tc.expError == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expError)
		}
		// if there was no TPM connection error, we should have tried to lock it
		if tc.tpmErr == nil {
			c.Assert(sbBlockPCRProtectionPolicesCalls, Equals, 1)
		} else {
			c.Assert(sbBlockPCRProtectionPolicesCalls, Equals, 0)
		}
	}
}

func (s *secbootSuite) TestProvisionForCVM(c *C) {
	mockTpm, restore := mockSbTPMConnection(c, nil)
	defer restore()

	restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
		c.Check(tpm, Equals, mockTpm)
		return true
	})
	defer restore()

	expectedTemplate := &tpm2.Public{
		Type:    tpm2.ObjectTypeRSA,
		NameAlg: tpm2.HashAlgorithmSHA256,
		Attrs: tpm2.AttrFixedTPM | tpm2.AttrFixedParent | tpm2.AttrSensitiveDataOrigin | tpm2.AttrUserWithAuth | tpm2.AttrNoDA |
			tpm2.AttrRestricted | tpm2.AttrDecrypt,
		Params: &tpm2.PublicParamsU{
			RSADetail: &tpm2.RSAParams{
				Symmetric: tpm2.SymDefObject{
					Algorithm: tpm2.SymObjectAlgorithmAES,
					KeyBits:   &tpm2.SymKeyBitsU{Sym: 128},
					Mode:      &tpm2.SymModeU{Sym: tpm2.SymModeCFB}},
				Scheme:   tpm2.RSAScheme{Scheme: tpm2.RSASchemeNull},
				KeyBits:  2048,
				Exponent: 0}}}
	mu.MustCopyValue(&expectedTemplate, expectedTemplate)

	dir := c.MkDir()

	f, err := os.OpenFile(filepath.Join(dir, "tpm2-srk.tmpl"), os.O_RDWR|os.O_CREATE, 0600)
	c.Assert(err, IsNil)
	defer f.Close()
	mu.MustMarshalToWriter(f, mu.Sized(expectedTemplate))

	provisioningCalls := 0
	restore = secboot.MockSbTPMEnsureProvisionedWithCustomSRK(func(tpm *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, lockoutAuth []byte, srkTemplate *tpm2.Public) error {
		provisioningCalls += 1
		c.Check(tpm, Equals, mockTpm)
		c.Check(mode, Equals, sb_tpm2.ProvisionModeWithoutLockout)
		c.Check(lockoutAuth, IsNil)
		c.Check(srkTemplate, DeepEquals, expectedTemplate)
		return nil
	})
	defer restore()

	c.Check(secboot.ProvisionForCVM(dir), IsNil)
	c.Check(provisioningCalls, Equals, 1)
}

func (s *secbootSuite) TestProvisionForCVMNoTPM(c *C) {
	_, restore := mockSbTPMConnection(c, sb_tpm2.ErrNoTPM2Device)
	defer restore()

	restore = secboot.MockSbTPMEnsureProvisionedWithCustomSRK(func(tpm *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, lockoutAuth []byte, srkTemplate *tpm2.Public) error {
		c.Error("unexpected provisioning call")
		return nil
	})
	defer restore()

	c.Check(secboot.ProvisionForCVM(c.MkDir()), IsNil)
}

func (s *secbootSuite) TestProvisionForCVMTPMNotEnabled(c *C) {
	mockTpm, restore := mockSbTPMConnection(c, nil)
	defer restore()

	restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
		c.Check(tpm, Equals, mockTpm)
		return false
	})
	defer restore()

	restore = secboot.MockSbTPMEnsureProvisionedWithCustomSRK(func(tpm *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, lockoutAuth []byte, srkTemplate *tpm2.Public) error {
		c.Error("unexpected provisioning call")
		return nil
	})
	defer restore()

	c.Check(secboot.ProvisionForCVM(c.MkDir()), IsNil)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncrypted(c *C) {

	// setup mock disks to use for locating the partition
	// restore := disks.MockMountPointDisksToPartitionMapping()
	// defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "name-enc",
				PartitionUUID:   "enc-dev-partuuid",
				FilesystemUUID:  "enc-dev-uuid",
			},
		},
	}

	mockDiskWithoutAnyDev := &disks.MockDiskMapping{}

	mockDiskWithUnencDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "name",
				PartitionUUID:   "unenc-dev-partuuid",
				FilesystemUUID:  "unenc-dev-uuid",
			},
		},
	}

	for idx, tc := range []struct {
		tpmEnabled          bool  // TPM storage and endorsement hierarchies disabled, only relevant if TPM available
		hasEncdev           bool  // an encrypted device exists
		rkAllow             bool  // allow recovery key activation
		rkErr               error // recovery key unlock error, only relevant if TPM not available
		activateErr         error // the activation error
		uuidFailure         bool  // failure to get valid uuid
		err                 string
		skipDiskEnsureCheck bool // whether to check to ensure the mock disk contains the device label
		expUnlockMethod     secboot.UnlockMethod
		disk                *disks.MockDiskMapping
		oldKeyFormat        bool
		noKeyFile           bool // when no key file is present, then we expect the key data is in the token
		errorReadKeyFile    bool
	}{
		{
			// happy case with tpm and encrypted device
			tpmEnabled: true, hasEncdev: true,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithSealedKey,
		}, {
			// happy case with tpm and encrypted device, and keys on the tokens
			tpmEnabled: true, hasEncdev: true,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithSealedKey,
			noKeyFile:       true,
		}, {
			// happy case with tpm and old sealed key
			tpmEnabled: true, hasEncdev: true,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithSealedKey,
			oldKeyFormat:    true,
		}, {
			// If we cannot read the key file we should still try to open without key
			tpmEnabled: true, hasEncdev: true,
			disk:             mockDiskWithEncDev,
			expUnlockMethod:  secboot.UnlockedWithSealedKey,
			errorReadKeyFile: true,
		}, {
			// encrypted device: failure to generate uuid based target device name
			tpmEnabled: true, hasEncdev: true, uuidFailure: true,
			disk: mockDiskWithEncDev,
			err:  "mocked uuid error",
		}, {
			// device activation fails
			tpmEnabled: true, hasEncdev: true,
			activateErr: fmt.Errorf("activation error"),
			err:         "cannot activate encrypted device .*: activation error",
			disk:        mockDiskWithEncDev,
		}, {
			// device activation fails
			tpmEnabled: true, hasEncdev: true,
			activateErr: fmt.Errorf("activation error"),
			err:         "cannot activate encrypted device .*: activation error",
			disk:        mockDiskWithEncDev,
		}, {
			// happy case without encrypted device
			tpmEnabled: true,
			disk:       mockDiskWithUnencDev,
		}, {
			// happy case with tpm and encrypted device, activation
			// with recovery key
			tpmEnabled: true, hasEncdev: true,
			activateErr:     sb.ErrRecoveryKeyUsed,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithRecoveryKey,
		}, {
			// tpm disabled, no encrypted device
			disk: mockDiskWithUnencDev,
		}, {
			// tpm disabled, has encrypted device, unlocked using the recovery key
			hasEncdev:       true,
			rkAllow:         true,
			activateErr:     sb.ErrRecoveryKeyUsed,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithRecoveryKey,
		}, {
			// tpm disabled, has encrypted device, recovery key unlocking fails
			hasEncdev: true, rkErr: errors.New("cannot unlock with recovery key"),
			rkAllow:     true,
			activateErr: fmt.Errorf("some error"),
			disk:        mockDiskWithEncDev,
			err:         `cannot activate encrypted device "/dev/disk/by-uuid/enc-dev-uuid": some error`,
		}, {
			// no disks at all
			disk:                mockDiskWithoutAnyDev,
			skipDiskEnsureCheck: true,
			// error is specifically for failing to find name, NOT name-enc, we
			// will properly fall back to looking for name if we didn't find
			// name-enc
			err: "error enumerating partitions for disk to find unencrypted device \"name\": filesystem label \"name\" not found",
		},
	} {
		// we need a closure to force calling the "defer"s at the end of the test case
		func() {
			c.Logf("tc %v: %+v", idx, tc)

			logbuf, restore := logger.MockLogger()
			defer restore()

			randomUUID := fmt.Sprintf("random-uuid-for-test-%d", idx)
			restore = secboot.MockRandomKernelUUID(func() (string, error) {
				if tc.uuidFailure {
					return "", errors.New("mocked uuid error")
				}
				return randomUUID, nil
			})
			defer restore()

			restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
				return tc.tpmEnabled
			})
			defer restore()

			defaultDevice := "name"

			fsLabel := defaultDevice
			if tc.hasEncdev {
				fsLabel += "-enc"
			}

			uuid := ""
			partUUID := ""
			if !tc.skipDiskEnsureCheck {
				for _, p := range tc.disk.Structure {
					if p.FilesystemLabel == fsLabel {
						uuid = p.FilesystemUUID
						partUUID = p.PartitionUUID
						break
					}
				}
				c.Assert(uuid, Not(Equals), "", Commentf("didn't find fs label %s in disk", fsLabel))
				c.Assert(partUUID, Not(Equals), "", Commentf("didn't find fs label %s in disk", fsLabel))
			}

			devicePath := filepath.Join("/dev/disk/by-partuuid", partUUID)
			devicePathUUID := fmt.Sprintf("/dev/disk/by-uuid/%s", uuid)

			var keyPath string
			var expectedKeyPath fs.FileInfo

			if !tc.noKeyFile && !tc.errorReadKeyFile {
				if tc.oldKeyFormat {
					keyPath = filepath.Join("test-data", "keyfile")
				} else {
					keyPath = filepath.Join("test-data", "keydata")
				}
				finfo, err := os.Lstat(keyPath)
				c.Assert(err, IsNil)
				expectedKeyPath = finfo
			} else {
				keyPath = "/some/path"
			}

			var expectedKeyData *sb.KeyData

			restore = secboot.MockSbNewKeyDataFromSealedKeyObjectFile(func(path string) (*sb.KeyData, error) {
				if !tc.oldKeyFormat {
					c.Errorf("unexpected call")
				}
				info, err := os.Lstat(path)
				c.Assert(err, IsNil)
				sameFile := os.SameFile(expectedKeyPath, info)
				c.Check(sameFile, Equals, true)

				kd, err := sb_tpm2.NewKeyDataFromSealedKeyObjectFile(keyPath)
				c.Assert(err, IsNil)

				if sameFile {
					c.Check(expectedKeyData, IsNil)
					expectedKeyData = kd
				}

				return kd, nil
			})
			defer restore()

			var expectedKeyDataReader *sb.FileKeyDataReader

			restore = secboot.MockSbNewFileKeyDataReader(func(path string) (*sb.FileKeyDataReader, error) {
				if tc.oldKeyFormat || tc.noKeyFile || tc.errorReadKeyFile {
					c.Errorf("unexpected call")
				}
				info, err := os.Lstat(path)
				c.Assert(err, IsNil)
				sameFile := os.SameFile(expectedKeyPath, info)
				c.Check(sameFile, Equals, true)

				kdr, err := sb.NewFileKeyDataReader(keyPath)
				c.Assert(err, IsNil)

				if sameFile {
					c.Check(expectedKeyDataReader, IsNil)
					expectedKeyDataReader = kdr
				}

				return kdr, nil
			})
			defer restore()

			if tc.noKeyFile || tc.errorReadKeyFile {
				restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
					if tc.noKeyFile {
						return fs.ErrNotExist
					}
					if tc.errorReadKeyFile {
						return fmt.Errorf("some other error")
					}
					c.Errorf("unexpected call")
					return fmt.Errorf("unexpected call")
				})
				defer restore()
			}

			restore = secboot.MockSbReadKeyData(func(reader sb.KeyDataReader) (*sb.KeyData, error) {
				if tc.oldKeyFormat || tc.noKeyFile || tc.errorReadKeyFile {
					c.Errorf("unexpected call")
				}
				c.Check(expectedKeyDataReader, Equals, reader)
				kd, err := sb.ReadKeyData(reader)
				c.Assert(err, IsNil)

				if expectedKeyDataReader == reader {
					c.Check(expectedKeyData, IsNil)
					expectedKeyData = kd
				}

				return kd, nil
			})
			defer restore()

			restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {

				c.Assert(volumeName, Equals, "name-"+randomUUID)
				c.Assert(sourceDevicePath, Equals, devicePathUUID)
				if tc.noKeyFile || tc.errorReadKeyFile {
					c.Check(keys, HasLen, 0)
				} else {
					c.Assert(keys, HasLen, 1)
					c.Assert(keys[0], Equals, expectedKeyData)
				}

				if tc.rkAllow {
					c.Assert(*options, DeepEquals, sb.ActivateVolumeOptions{
						PassphraseTries:   1,
						RecoveryKeyTries:  3,
						KeyringPrefix:     "ubuntu-fde",
						LegacyDevicePaths: []string{"/dev/disk/by-partuuid/enc-dev-partuuid"},
					})
				} else {
					c.Assert(*options, DeepEquals, sb.ActivateVolumeOptions{
						PassphraseTries: 1,
						// activation with recovery key was disabled
						RecoveryKeyTries:  0,
						KeyringPrefix:     "ubuntu-fde",
						LegacyDevicePaths: []string{"/dev/disk/by-partuuid/enc-dev-partuuid"},
					})
				}
				return tc.activateErr
			})
			defer restore()

			restore = secboot.MockSbActivateVolumeWithRecoveryKey(func(name, device string, authReq sb.AuthRequestor,
				options *sb.ActivateVolumeOptions) error {
				c.Errorf("unexpected call")
				return fmt.Errorf("unexpected call")
			})
			defer restore()

			modeSet := 0
			restore = secboot.MockSbSetBootMode(func(mode string) {
				modeSet++
				switch modeSet {
				case 1:
					c.Check(mode, Equals, "some-weird-mode")
				case 2:
					c.Check(mode, Equals, "")
				default:
					c.Error("mode set a third time?")
				}
			})
			defer restore()

			opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: tc.rkAllow,
				WhichModel: func() (*asserts.Model, error) {
					return fakeModel, nil
				},
				BootMode: "some-weird-mode",
			}
			unlockRes, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(tc.disk, defaultDevice, keyPath, opts)
			if tc.errorReadKeyFile {
				c.Check(logbuf.String(), testutil.Contains, `WARNING: there was an error loading key /some/path: some other error`)
			} else {
				c.Check(logbuf.String(), Not(testutil.Contains), `WARNING: there was an error loading key`)
			}
			if tc.err == "" {
				c.Assert(err, IsNil)
				c.Assert(unlockRes.IsEncrypted, Equals, tc.hasEncdev)
				c.Assert(unlockRes.PartDevice, Equals, devicePath)
				if tc.hasEncdev {
					c.Check(modeSet, Equals, 2)
					c.Assert(unlockRes.FsDevice, Equals, filepath.Join("/dev/mapper", defaultDevice+"-"+randomUUID))
				} else {
					c.Assert(unlockRes.FsDevice, Equals, devicePath)
				}
			} else {
				c.Assert(err, ErrorMatches, tc.err)
				// also check that the IsEncrypted value matches, this is
				// important for robust callers to know whether they should try to
				// unlock using a different method or not
				// this is only skipped on some test cases where we get an error
				// very early, like trying to connect to the tpm
				c.Assert(unlockRes.IsEncrypted, Equals, tc.hasEncdev)
				if tc.hasEncdev {
					c.Check(unlockRes.PartDevice, Equals, devicePath)
					c.Check(unlockRes.FsDevice, Equals, "")
				} else {
					c.Check(unlockRes.PartDevice, Equals, "")
					c.Check(unlockRes.FsDevice, Equals, "")
				}
			}

			c.Assert(unlockRes.UnlockMethod, Equals, tc.expUnlockMethod)
		}()
	}
}

func (s *secbootSuite) TestEFIImageFromBootFile(c *C) {
	tmpDir := c.MkDir()

	// set up some test files
	existingFile := filepath.Join(tmpDir, "foo")
	err := os.WriteFile(existingFile, nil, 0644)
	c.Assert(err, IsNil)
	missingFile := filepath.Join(tmpDir, "bar")
	snapFile := filepath.Join(tmpDir, "test.snap")
	snapf, err := createMockSnapFile(c.MkDir(), snapFile, "app")

	for _, tc := range []struct {
		bootFile bootloader.BootFile
		efiImage sb_efi.Image
		err      string
	}{
		{
			// happy case for EFI image
			bootFile: bootloader.NewBootFile("", existingFile, bootloader.RoleRecovery),
			efiImage: sb_efi.NewFileImage(existingFile),
		},
		{
			// missing EFI image
			bootFile: bootloader.NewBootFile("", missingFile, bootloader.RoleRecovery),
			err:      fmt.Sprintf("file %s/bar does not exist", tmpDir),
		},
		{
			// happy case for snap file
			bootFile: bootloader.NewBootFile(snapFile, "rel", bootloader.RoleRecovery),
			efiImage: sb_efi.NewSnapFileImage(snapf, "rel"),
		},
		{
			// invalid snap file
			bootFile: bootloader.NewBootFile(existingFile, "rel", bootloader.RoleRecovery),
			err:      fmt.Sprintf(`cannot process snap or snapdir: cannot read "%s/foo": EOF`, tmpDir),
		},
		{
			// missing snap file
			bootFile: bootloader.NewBootFile(missingFile, "rel", bootloader.RoleRecovery),
			err:      fmt.Sprintf(`cannot process snap or snapdir: open %s/bar: no such file or directory`, tmpDir),
		},
	} {
		o, err := secboot.EFIImageFromBootFile(&tc.bootFile)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(o, DeepEquals, tc.efiImage)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *secbootSuite) TestProvisionTPM(c *C) {
	mockErr := errors.New("some error")

	for idx, tc := range []struct {
		tpmErr            error
		tpmEnabled        bool
		mode              secboot.TPMProvisionMode
		writeLockoutAuth  bool
		provisioningErr   error
		provisioningCalls int
		expectedErr       string
	}{
		{
			tpmErr: mockErr, mode: secboot.TPMProvisionFull,
			expectedErr: "cannot connect to TPM: some error",
		}, {
			tpmEnabled: false, mode: secboot.TPMProvisionFull, expectedErr: "TPM device is not enabled",
		}, {
			tpmEnabled: true, mode: secboot.TPMProvisionFull, provisioningErr: mockErr,
			provisioningCalls: 1, expectedErr: "cannot provision TPM: some error",
		}, {
			tpmEnabled: true, mode: secboot.TPMPartialReprovision, provisioningCalls: 0,
			expectedErr: "cannot read existing lockout auth: open .*/lockout-auth: no such file or directory",
		},
		// happy cases
		{
			tpmEnabled: true, mode: secboot.TPMProvisionFull, provisioningCalls: 1,
		}, {
			tpmEnabled: true, mode: secboot.TPMPartialReprovision, writeLockoutAuth: true,
			provisioningCalls: 1,
		},
	} {
		c.Logf("tc: %v", idx)
		d := c.MkDir()
		tpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		// mock TPM enabled check
		restore = secboot.MockIsTPMEnabled(func(t *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		lockoutAuthData := []byte{'l', 'o', 'c', 'k', 'o', 'u', 't', 1, 1, 1, 1, 1, 1, 1, 1, 1}
		if tc.writeLockoutAuth {
			c.Assert(os.WriteFile(filepath.Join(d, "lockout-auth"), lockoutAuthData, 0644), IsNil)
		}

		// mock provisioning
		provisioningCalls := 0
		restore = secboot.MockSbTPMEnsureProvisioned(func(t *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, newLockoutAuth []byte) error {
			provisioningCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(mode, Equals, sb_tpm2.ProvisionModeFull)
			return tc.provisioningErr
		})
		defer restore()

		err := secboot.ProvisionTPM(tc.mode, filepath.Join(d, "lockout-auth"))
		if tc.expectedErr != "" {
			c.Assert(err, ErrorMatches, tc.expectedErr)
		} else {
			c.Assert(err, IsNil)
		}
		c.Check(provisioningCalls, Equals, tc.provisioningCalls)
	}

}

func mockAuthOptions(mode device.AuthMode, kdfType string) *device.VolumesAuthOptions {
	return &device.VolumesAuthOptions{
		Mode:       mode,
		KDFType:    kdfType,
		KDFTime:    200 * time.Millisecond,
		Passphrase: "test",
	}
}

func (s *secbootSuite) TestSealKey(c *C) {
	mockErr := errors.New("some error")

	for idx, tc := range []struct {
		tpmErr               error
		tpmEnabled           bool
		volumesAuth          *device.VolumesAuthOptions
		missingFile          bool
		badSnapFile          bool
		addPCRProfileErr     error
		addSystemdEFIStubErr error
		addSnapModelErr      error
		provisioningErr      error
		sealErr              error
		sealCalls            int
		passphraseSealCalls  int
		expectedErr          string
		saveToFile           bool
	}{
		{tpmErr: mockErr, expectedErr: "cannot connect to TPM: some error"},
		{tpmEnabled: false, expectedErr: "TPM device is not enabled"},
		{tpmEnabled: true, missingFile: true, expectedErr: "cannot build EFI image load sequences: file /does/not/exist does not exist"},
		{tpmEnabled: true, badSnapFile: true, expectedErr: `cannot build EFI image load sequences: cannot process snap or snapdir: cannot read .*\/kernel.snap": EOF`},
		{tpmEnabled: true, addPCRProfileErr: mockErr, expectedErr: "cannot add EFI secure boot and boot manager policy profiles: some error"},
		{tpmEnabled: true, addSystemdEFIStubErr: mockErr, expectedErr: "cannot add systemd EFI stub profile: some error"},
		{tpmEnabled: true, addSnapModelErr: mockErr, expectedErr: "cannot add snap model profile: some error"},
		{tpmEnabled: true, sealErr: mockErr, sealCalls: 1, expectedErr: "some error"},
		{tpmEnabled: true, sealCalls: 2, expectedErr: ""},
		{tpmEnabled: true, sealCalls: 2, expectedErr: "", saveToFile: true},
		{tpmEnabled: true, passphraseSealCalls: 2, volumesAuth: mockAuthOptions("passphrase", ""), expectedErr: ""},
		{tpmEnabled: true, passphraseSealCalls: 2, volumesAuth: mockAuthOptions("passphrase", "argon2i"), expectedErr: ""},
		{tpmEnabled: true, passphraseSealCalls: 2, volumesAuth: mockAuthOptions("passphrase", "argon2id"), expectedErr: ""},
		{tpmEnabled: true, passphraseSealCalls: 2, volumesAuth: mockAuthOptions("passphrase", "pbkdf2"), expectedErr: ""},
		{tpmEnabled: true, volumesAuth: mockAuthOptions("passphrase", "bad-kdf"), expectedErr: `internal error: unknown kdfType passed "bad-kdf"`},
		{tpmEnabled: true, sealErr: mockErr, passphraseSealCalls: 1, volumesAuth: mockAuthOptions("passphrase", ""), expectedErr: "some error"},
		{tpmErr: mockErr, volumesAuth: mockAuthOptions("passphrase", ""), expectedErr: `cannot connect to TPM: some error`},
		{tpmEnabled: true, volumesAuth: mockAuthOptions("pin", ""), expectedErr: `"pin" authentication mode is not implemented`},
		{tpmEnabled: true, volumesAuth: mockAuthOptions("bad-mode", ""), expectedErr: `internal error: invalid authentication mode "bad-mode"`},
	} {
		c.Logf("tc: %v", idx)
		tmpDir := c.MkDir()
		var mockBF []bootloader.BootFile
		for _, name := range []string{"a", "b", "c", "d"} {
			mockFileName := filepath.Join(tmpDir, name)
			err := os.WriteFile(mockFileName, nil, 0644)
			c.Assert(err, IsNil)
			mockBF = append(mockBF, bootloader.NewBootFile("", mockFileName, bootloader.RoleRecovery))
		}

		if tc.missingFile {
			mockBF[0].Path = "/does/not/exist"
		}

		var kernelSnap snap.Container
		snapPath := filepath.Join(tmpDir, "kernel.snap")
		if tc.badSnapFile {
			err := os.WriteFile(snapPath, nil, 0644)
			c.Assert(err, IsNil)
		} else {
			var err error
			kernelSnap, err = createMockSnapFile(c.MkDir(), snapPath, "kernel")
			c.Assert(err, IsNil)
		}

		mockBF = append(mockBF, bootloader.NewBootFile(snapPath, "kernel.efi", bootloader.RoleRecovery))

		myParams := secboot.SealKeysParams{
			ModelParams: []*secboot.SealKeyModelParams{
				{
					EFILoadChains: []*secboot.LoadChain{
						secboot.NewLoadChain(mockBF[0],
							secboot.NewLoadChain(mockBF[4])),
					},
					KernelCmdlines: []string{"cmdline1"},
					Model:          &asserts.Model{},
				},
				{
					EFILoadChains: []*secboot.LoadChain{
						secboot.NewLoadChain(mockBF[0],
							secboot.NewLoadChain(mockBF[2],
								secboot.NewLoadChain(mockBF[4])),
							secboot.NewLoadChain(mockBF[3],
								secboot.NewLoadChain(mockBF[4]))),
						secboot.NewLoadChain(mockBF[1],
							secboot.NewLoadChain(mockBF[2],
								secboot.NewLoadChain(mockBF[4])),
							secboot.NewLoadChain(mockBF[3],
								secboot.NewLoadChain(mockBF[4]))),
					},
					KernelCmdlines: []string{"cmdline2", "cmdline3"},
					Model:          &asserts.Model{},
				},
			},
			TPMPolicyAuthKeyFile:   filepath.Join(tmpDir, "policy-auth-key-file"),
			PCRPolicyCounterHandle: 42,
			VolumesAuth:            tc.volumesAuth,
			KeyRole:                "somerole",
		}

		containerA := secboot.CreateMockBootstrappedContainer()
		containerB := secboot.CreateMockBootstrappedContainer()
		myKeys := []secboot.SealKeyRequest{
			{
				BootstrappedContainer: containerA,
				SlotName:              "foo1",
			},
			{
				BootstrappedContainer: containerB,
				SlotName:              "foo2",
			},
		}

		if tc.saveToFile {
			myKeys[0].KeyFile = filepath.Join(tmpDir, "key-file-1")
			myKeys[1].KeyFile = filepath.Join(tmpDir, "key-file-2")
		}

		// events for
		// a -> kernel
		sequences1 := sb_efi.NewImageLoadSequences().Append(
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockBF[0].Path),
			).Loads(sb_efi.NewImageLoadActivity(
				sb_efi.NewSnapFileImage(
					kernelSnap,
					"kernel.efi",
				),
			)),
		)

		// "cdk" events for
		// c -> kernel OR
		// d -> kernel
		cdk := []sb_efi.ImageLoadActivity{
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockBF[2].Path),
			).Loads(sb_efi.NewImageLoadActivity(
				sb_efi.NewSnapFileImage(
					kernelSnap,
					"kernel.efi",
				),
			)),
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockBF[3].Path),
			).Loads(sb_efi.NewImageLoadActivity(
				sb_efi.NewSnapFileImage(
					kernelSnap,
					"kernel.efi",
				),
			)),
		}

		// events for
		// a -> "cdk"
		// b -> "cdk"
		sequences2 := sb_efi.NewImageLoadSequences().Append(
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockBF[0].Path),
			).Loads(cdk...),
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockBF[1].Path),
			).Loads(cdk...),
		)

		tpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		// mock adding EFI secure boot policy profile

		var rootBranch *sb_tpm2.PCRProtectionProfileBranch
		addPCRProfileCalls := 0
		restore = secboot.MockSbEfiAddPCRProfile(func(pcrAlg tpm2.HashAlgorithmId, branch *sb_tpm2.PCRProtectionProfileBranch, loadSequences *sb_efi.ImageLoadSequences, options ...sb_efi.PCRProfileOption) error {
			addPCRProfileCalls++
			rootBranch = branch
			c.Assert(pcrAlg, Equals, tpm2.HashAlgorithmSHA256)
			switch addPCRProfileCalls {
			case 1:
				c.Assert(loadSequences, DeepEquals, sequences1)
			case 2:
				c.Assert(loadSequences, DeepEquals, sequences2)
			default:
				c.Error("AddPCRProfile shouldn't be called a third time")
			}
			return tc.addPCRProfileErr
		})
		defer restore()

		// mock adding systemd EFI stub profile
		addSystemdEfiStubCalls := 0
		restore = secboot.MockSbEfiAddSystemdStubProfile(func(profile *sb_tpm2.PCRProtectionProfileBranch, params *sb_efi.SystemdStubProfileParams) error {
			addSystemdEfiStubCalls++
			c.Assert(profile, Equals, rootBranch)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.PCRIndex, Equals, 12)
			switch addSystemdEfiStubCalls {
			case 1:
				c.Assert(params.KernelCmdlines, DeepEquals, myParams.ModelParams[0].KernelCmdlines)
			case 2:
				c.Assert(params.KernelCmdlines, DeepEquals, myParams.ModelParams[1].KernelCmdlines)
			default:
				c.Error("AddSystemdStubProfile shouldn't be called a third time")
			}
			return tc.addSystemdEFIStubErr
		})
		defer restore()

		// mock adding snap model profile
		addSnapModelCalls := 0
		restore = secboot.MockSbAddSnapModelProfile(func(profile *sb_tpm2.PCRProtectionProfileBranch, params *sb_tpm2.SnapModelProfileParams) error {
			addSnapModelCalls++
			c.Assert(profile, Equals, rootBranch)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.PCRIndex, Equals, 12)
			switch addSnapModelCalls {
			case 1:
				c.Assert(params.Models[0], DeepEquals, myParams.ModelParams[0].Model)
			case 2:
				c.Assert(params.Models[0], DeepEquals, myParams.ModelParams[1].Model)
			default:
				c.Error("AddSnapModelProfile shouldn't be called a third time")
			}
			return tc.addSnapModelErr
		})
		defer restore()

		// mock sealing
		sealCalls := 0
		restore = secboot.MockSbNewTPMProtectedKey(func(t *sb_tpm2.Connection, params *sb_tpm2.ProtectKeyParams) (protectedKey *sb.KeyData, primaryKey sb.PrimaryKey, unlockKey sb.DiskUnlockKey, err error) {
			sealCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(params.PCRPolicyCounterHandle, Equals, tpm2.Handle(42))
			c.Check(params.Role, Equals, "somerole")
			return &sb.KeyData{}, sb.PrimaryKey{}, sb.DiskUnlockKey{}, tc.sealErr
		})
		defer restore()
		passphraseSealCalls := 0
		restore = secboot.MockSbNewTPMPassphraseProtectedKey(func(t *sb_tpm2.Connection, params *sb_tpm2.PassphraseProtectKeyParams, passphrase string) (protectedKey *sb.KeyData, primaryKey sb.PrimaryKey, unlockKey sb.DiskUnlockKey, err error) {
			passphraseSealCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(params.PCRPolicyCounterHandle, Equals, tpm2.Handle(42))
			c.Check(params.Role, Equals, "somerole")
			var expectedKDFOptions sb.KDFOptions
			switch tc.volumesAuth.KDFType {
			case "argon2id":
				expectedKDFOptions = &sb.Argon2Options{Mode: sb.Argon2id, TargetDuration: tc.volumesAuth.KDFTime}
			case "argon2i":
				expectedKDFOptions = &sb.Argon2Options{Mode: sb.Argon2i, TargetDuration: tc.volumesAuth.KDFTime}
			case "pbkdf2":
				expectedKDFOptions = &sb.PBKDF2Options{TargetDuration: tc.volumesAuth.KDFTime}
			}
			c.Assert(params.KDFOptions, DeepEquals, expectedKDFOptions)
			c.Assert(passphrase, Equals, tc.volumesAuth.Passphrase)
			return &sb.KeyData{}, sb.PrimaryKey{}, sb.DiskUnlockKey{}, tc.sealErr
		})
		defer restore()

		// mock TPM enabled check
		restore = secboot.MockIsTPMEnabled(func(t *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		_, err := secboot.SealKeys(myKeys, &myParams)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
			c.Assert(addPCRProfileCalls, Equals, 2)
			c.Assert(addSnapModelCalls, Equals, 2)
			c.Assert(osutil.FileExists(myParams.TPMPolicyAuthKeyFile), Equals, true)

			_, aHasSlot := containerA.Slots["foo1"]
			c.Check(aHasSlot, Equals, true)
			_, bHasSlot := containerB.Slots["foo2"]
			c.Check(bHasSlot, Equals, true)
			if tc.saveToFile {
				c.Check(containerA.Tokens, HasLen, 0)
				c.Check(containerB.Tokens, HasLen, 0)
				c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-1")), Equals, true)
				c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-2")), Equals, true)
			} else {
				c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-1")), Equals, false)
				c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-2")), Equals, false)
				_, aHasToken := containerA.Tokens["foo1"]
				c.Check(aHasToken, Equals, true)
				_, bHasToken := containerB.Tokens["foo2"]
				c.Check(bHasToken, Equals, true)
			}
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
		}
		c.Assert(sealCalls, Equals, tc.sealCalls)
		c.Assert(passphraseSealCalls, Equals, tc.passphraseSealCalls)

	}
}

func (s *secbootSuite) TestResealKey(c *C) {
	mockErr := errors.New("some error")

	for idx, tc := range []struct {
		tpmErr                 error
		tpmEnabled             bool
		usePrimaryKeyFile      bool
		keyDataInFile          bool
		missingFile            bool
		addPCRProfileErr       error
		addSystemdEFIStubErr   error
		addSnapModelErr        error
		readSealedKeyObjectErr error
		provisioningErr        error
		resealErr              error
		resealCalls            int
		revokeErr              error
		revokeCalls            int
		expectedErr            string
		oldKeyFiles            bool
		buildProfileErr        string
		dbxUpdate              []byte
	}{
		// happy case
		{tpmEnabled: true, resealCalls: 1},
		// happy case with key files
		{tpmEnabled: true, keyDataInFile: true, usePrimaryKeyFile: true, resealCalls: 1},
		// happy case with DBX update
		{tpmEnabled: true, resealCalls: 1, dbxUpdate: []byte("dbx-update")},
		// happy case, old keys
		{tpmEnabled: true, resealCalls: 1, revokeCalls: 1, oldKeyFiles: true},

		// unhappy cases
		{tpmErr: mockErr, expectedErr: "cannot connect to TPM: some error"},
		{tpmEnabled: false, expectedErr: "TPM device is not enabled"},
		{tpmEnabled: true, missingFile: true, buildProfileErr: `cannot build EFI image load sequences: file .*\/file.efi does not exist`},
		{tpmEnabled: true, addPCRProfileErr: mockErr, buildProfileErr: `cannot add EFI secure boot and boot manager policy profiles: some error`},
		{tpmEnabled: true, addSystemdEFIStubErr: mockErr, buildProfileErr: `cannot add systemd EFI stub profile: some error`},
		{tpmEnabled: true, addSnapModelErr: mockErr, buildProfileErr: `cannot add snap model profile: some error`},
		{tpmEnabled: true, readSealedKeyObjectErr: mockErr, expectedErr: `trying to load key data from .* returned "some error", and from .* returned "some error"`},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "cannot update legacy PCR protection policy: some error", oldKeyFiles: true},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "cannot update PCR protection policy: some error"},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "cannot update legacy PCR protection policy: some error", oldKeyFiles: true},
		{tpmEnabled: true, revokeErr: errors.New("revoke error"), resealCalls: 1, revokeCalls: 1, expectedErr: "cannot revoke old PCR protection policies: revoke error", oldKeyFiles: true},
	} {
		c.Logf("reseal tc[%v]: %+v", idx, tc)

		mockTPMPolicyAuthKey := []byte{1, 3, 3, 7}
		mockTPMPolicyAuthKeyFile := filepath.Join(c.MkDir(), "policy-auth-key-file")
		if tc.usePrimaryKeyFile {
			err := os.WriteFile(mockTPMPolicyAuthKeyFile, mockTPMPolicyAuthKey, 0600)
			c.Assert(err, IsNil)
		}
		defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
			c.Check(prefix, Equals, "ubuntu-fde")
			c.Check(devicePath, Equals, "/dev/somedevice")
			c.Check(remove, Equals, false)
			if tc.usePrimaryKeyFile {
				return nil, sb.ErrKernelKeyNotFound
			} else {
				return []byte{1, 3, 3, 7}, nil
			}
		})()

		mockEFI := bootloader.NewBootFile("", filepath.Join(c.MkDir(), "file.efi"), bootloader.RoleRecovery)
		if !tc.missingFile {
			err := os.WriteFile(mockEFI.Path, nil, 0644)
			c.Assert(err, IsNil)
		}

		modelParams := []*secboot.SealKeyModelParams{
			{
				EFILoadChains:         []*secboot.LoadChain{secboot.NewLoadChain(mockEFI)},
				KernelCmdlines:        []string{"cmdline"},
				Model:                 &asserts.Model{},
				EFISignatureDbxUpdate: tc.dbxUpdate,
			},
		}

		sequences := sb_efi.NewImageLoadSequences().Append(
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockEFI.Path),
			),
		)

		var dbUpdateOption sb_efi.PCRProfileOption = sb_efi.WithSignatureDBUpdates()
		if len(tc.dbxUpdate) > 0 {
			dbUpdateOption = sb_efi.WithSignatureDBUpdates([]*sb_efi.SignatureDBUpdate{
				{Name: sb_efi.Dbx, Data: tc.dbxUpdate},
			}...)
		}

		addPCRProfileCalls := 0
		restore := secboot.MockSbEfiAddPCRProfile(func(pcrAlg tpm2.HashAlgorithmId, branch *sb_tpm2.PCRProtectionProfileBranch, loadSequences *sb_efi.ImageLoadSequences, options ...sb_efi.PCRProfileOption) error {
			addPCRProfileCalls++
			c.Assert(pcrAlg, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(loadSequences, DeepEquals, sequences)
			c.Assert(options, HasLen, 3)
			// TODO:FDEM: test other options

			// options are passed as an interface, and the underlying types are
			// not exported by secboot, so we simply assume that specific options
			// appear at certain indices, in this case, DBX is added as a last
			// option
			c.Assert(options[len(options)-1], DeepEquals, dbUpdateOption)

			return tc.addPCRProfileErr
		})
		defer restore()

		// mock adding snap model profile
		addSnapModelCalls := 0
		restore = secboot.MockSbAddSnapModelProfile(func(profile *sb_tpm2.PCRProtectionProfileBranch, params *sb_tpm2.SnapModelProfileParams) error {
			addSnapModelCalls++
			//c.Assert(profile, Equals, pcrProfile)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.PCRIndex, Equals, 12)
			c.Assert(params.Models[0], DeepEquals, modelParams[0].Model)
			return tc.addSnapModelErr
		})
		defer restore()

		// mock adding systemd EFI stub profile
		addSystemdEfiStubCalls := 0
		restore = secboot.MockSbEfiAddSystemdStubProfile(func(profile *sb_tpm2.PCRProtectionProfileBranch, params *sb_efi.SystemdStubProfileParams) error {
			addSystemdEfiStubCalls++
			//c.Assert(profile, Equals, pcrProfile)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.PCRIndex, Equals, 12)
			c.Assert(params.KernelCmdlines, DeepEquals, modelParams[0].KernelCmdlines)
			return tc.addSystemdEFIStubErr
		})
		defer restore()

		pcrProfile, err := secboot.BuildPCRProtectionProfile(modelParams)
		if len(tc.buildProfileErr) > 0 {
			c.Assert(err, ErrorMatches, tc.buildProfileErr)
			continue
		} else {
			c.Assert(err, IsNil)
		}

		tmpdir := c.MkDir()
		keyFile := filepath.Join(tmpdir, "keyfile")
		keyFile2 := filepath.Join(tmpdir, "keyfile2")
		myParams := &secboot.ResealKeysParams{
			PCRProfile: pcrProfile,
			Keys: []secboot.KeyDataLocation{
				{
					DevicePath: "/dev/somedevice",
					SlotName:   "key1",
					KeyFile:    keyFile,
				},
				{
					DevicePath: "/dev/somedevice",
					SlotName:   "key2",
					KeyFile:    keyFile2,
				},
			},
			PrimaryKey: mockTPMPolicyAuthKey,
		}

		numMockSealedKeyObjects := len(myParams.Keys)
		mockSealedKeyObjects := make([]*sb_tpm2.SealedKeyObject, 0, numMockSealedKeyObjects)
		mockKeyDatas := make([]*sb.KeyData, 0, numMockSealedKeyObjects)
		for range myParams.Keys {
			if tc.oldKeyFiles {
				// Copy of
				// https://github.com/snapcore/secboot/blob/master/internal/compattest/testdata/v1/key
				// To create full looking
				// mockSealedKeyObjects, although {},{} would
				// have been enough as well
				mockSealedKeyFile := filepath.Join("test-data", "keyfile")
				mockSealedKeyObject, err := sb_tpm2.ReadSealedKeyObjectFromFile(mockSealedKeyFile)
				c.Assert(err, IsNil)
				mockSealedKeyObjects = append(mockSealedKeyObjects, mockSealedKeyObject)
			} else {
				mockSealedKeyFile := filepath.Join("test-data", "keydata")
				reader, err := sb.NewFileKeyDataReader(mockSealedKeyFile)
				c.Assert(err, IsNil)
				kd, err := sb.ReadKeyData(reader)
				c.Assert(err, IsNil)
				mockKeyDatas = append(mockKeyDatas, kd)
			}
		}

		// mock TPM connection
		tpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		// mock TPM enabled check
		restore = secboot.MockIsTPMEnabled(func(t *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		// mock ReadSealedKeyObject
		readSealedKeyObjectCalls := 0
		restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
			readSealedKeyObjectCalls++
			c.Check(hintExpectFDEHook, Equals, false)
			c.Assert(keyfile, Equals, myParams.Keys[readSealedKeyObjectCalls-1].KeyFile)
			if tc.oldKeyFiles {
				kl.LoadedSealedKeyObject(mockSealedKeyObjects[readSealedKeyObjectCalls-1])
				return tc.readSealedKeyObjectErr
			} else {
				kl.LoadedKeyData(mockKeyDatas[readSealedKeyObjectCalls-1])
				return tc.readSealedKeyObjectErr
			}
		})
		defer restore()

		readKeyTokenCalls := 0
		restore = secboot.MockReadKeyToken(func(devicePath, slotName string) (*sb.KeyData, error) {
			readKeyTokenCalls++
			c.Check(devicePath, Equals, "/dev/somedevice")
			if tc.keyDataInFile || tc.oldKeyFiles {
				return nil, fmt.Errorf("token does not work")
			} else {
				if tc.readSealedKeyObjectErr != nil {
					return nil, tc.readSealedKeyObjectErr
				}
				switch slotName {
				case "key1":
					return mockKeyDatas[0], nil
				case "key2":
					return mockKeyDatas[1], nil
				default:
					c.Errorf("unexpected call")
					return nil, fmt.Errorf("unexpected")
				}
			}
		})
		defer restore()

		keyData1Writer := &myKeyDataWriter{}
		keyData2Writer := &myKeyDataWriter{}
		tokenWritten := 0
		restore = secboot.MockNewLUKS2KeyDataWriter(func(devicePath string, name string) (secboot.KeyDataWriter, error) {
			tokenWritten++
			c.Check(devicePath, Equals, "/dev/somedevice")
			c.Check(tc.keyDataInFile || tc.oldKeyFiles, Equals, false)
			switch name {
			case "key1":
				return keyData1Writer, nil
			case "key2":
				return keyData2Writer, nil
			default:
				c.Errorf("unexpected call")
				return nil, fmt.Errorf("unexpected")
			}
		})
		defer restore()

		// mock PCR protection policy update
		resealCalls := 0
		restore = secboot.MockSbUpdateKeyPCRProtectionPolicyMultiple(func(t *sb_tpm2.Connection, keys []*sb_tpm2.SealedKeyObject, authKey sb.PrimaryKey, profile *sb_tpm2.PCRProtectionProfile) error {
			c.Assert(tc.oldKeyFiles, Equals, true)
			resealCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(keys, DeepEquals, mockSealedKeyObjects)
			c.Assert(authKey, DeepEquals, sb.PrimaryKey(mockTPMPolicyAuthKey))
			//c.Assert(profile, Equals, pcrProfile)
			return tc.resealErr
		})
		defer restore()
		// mock PCR protection policy revoke
		revokeCalls := 0
		restore = secboot.MockSbSealedKeyObjectRevokeOldPCRProtectionPolicies(func(sko *sb_tpm2.SealedKeyObject, t *sb_tpm2.Connection, authKey sb.PrimaryKey) error {
			c.Assert(tc.oldKeyFiles, Equals, true)
			revokeCalls++
			c.Assert(sko, Equals, mockSealedKeyObjects[0])
			c.Assert(t, Equals, tpm)
			c.Assert(authKey, DeepEquals, sb.PrimaryKey(mockTPMPolicyAuthKey))
			return tc.revokeErr
		})
		defer restore()

		restore = secboot.MockSbUpdateKeyDataPCRProtectionPolicy(func(t *sb_tpm2.Connection, authKey sb.PrimaryKey, pcrProfile *sb_tpm2.PCRProtectionProfile, policyVersionOption sb_tpm2.PCRPolicyVersionOption, keys ...*sb.KeyData) error {
			c.Assert(tc.oldKeyFiles, Equals, false)
			resealCalls++
			c.Check(authKey, DeepEquals, sb.PrimaryKey(mockTPMPolicyAuthKey))
			c.Check(tpm, Equals, tpm)
			c.Check(keys, DeepEquals, mockKeyDatas)
			return tc.resealErr
		})
		defer restore()

		err = secboot.ResealKeys(myParams)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
			c.Assert(addPCRProfileCalls, Equals, 1)
			c.Assert(addSystemdEfiStubCalls, Equals, 1)
			c.Assert(addSnapModelCalls, Equals, 1)
			if tc.keyDataInFile || tc.oldKeyFiles {
				c.Check(tokenWritten, Equals, 0)
				c.Assert(keyFile, testutil.FilePresent)
				c.Assert(keyFile2, testutil.FilePresent)
			} else {
				c.Check(tokenWritten, Equals, 2)
				c.Check(keyFile, Not(testutil.FilePresent))
				c.Check(keyFile2, Not(testutil.FilePresent))
			}
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr, Commentf("%v", tc))
			if revokeCalls == 0 {
				// files were not written out
				c.Assert(keyFile, testutil.FileAbsent)
				c.Assert(keyFile2, testutil.FileAbsent)
			}
		}
		c.Assert(resealCalls, Equals, tc.resealCalls)
		c.Assert(revokeCalls, Equals, tc.revokeCalls)
	}
}

func (s *secbootSuite) TestSealKeyNoModelParams(c *C) {
	myKeys := []secboot.SealKeyRequest{
		{
			BootstrappedContainer: secboot.CreateMockBootstrappedContainer(),
		},
	}
	myParams := secboot.SealKeysParams{
		TPMPolicyAuthKeyFile: "policy-auth-key-file",
	}

	_, err := secboot.SealKeys(myKeys, &myParams)
	c.Assert(err, ErrorMatches, "at least one set of model-specific parameters is required")
}

func createMockSnapFile(snapDir, snapPath, snapType string) (snap.Container, error) {
	snapYamlPath := filepath.Join(snapDir, "meta/snap.yaml")
	if err := os.MkdirAll(filepath.Dir(snapYamlPath), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(snapYamlPath, []byte("name: foo"), 0644); err != nil {
		return nil, err
	}
	sqfs := squashfs.New(snapPath)
	if err := sqfs.Build(snapDir, &squashfs.BuildOpts{SnapType: snapType}); err != nil {
		return nil, err
	}
	return snapfile.Open(snapPath)
}

func mockSbTPMConnection(c *C, tpmErr error) (*sb_tpm2.Connection, func()) {
	tcti, err := linux.OpenDevice("/dev/null")
	c.Assert(err, IsNil)
	tpmctx := tpm2.NewTPMContext(tcti)
	c.Assert(err, IsNil)
	tpm := &sb_tpm2.Connection{TPMContext: tpmctx}
	restore := secboot.MockSbConnectToDefaultTPM(func() (*sb_tpm2.Connection, error) {
		if tpmErr != nil {
			return nil, tpmErr
		}
		return tpm, nil
	})
	return tpm, restore
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingProtectorKeyBadDisk(c *C) {
	disk := &disks.MockDiskMapping{}
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingProtectorKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, ErrorMatches, `filesystem label "ubuntu-save-enc" not found`)
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{})
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingProtectorKeyUUIDError(c *C) {
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-save-enc",
				FilesystemUUID:  "321-321-321",
				PartitionUUID:   "123-123-123",
			},
		},
	}
	restore := secboot.MockRandomKernelUUID(func() (string, error) {
		return "", errors.New("mocked uuid error")
	})
	defer restore()

	unlockRes, err := secboot.UnlockEncryptedVolumeUsingProtectorKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, ErrorMatches, "mocked uuid error")
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		PartDevice:  "/dev/disk/by-uuid/321-321-321",
		IsEncrypted: true,
	})
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingProtectorKeyOldKeyHappy(c *C) {
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-save-enc",
				FilesystemUUID:  "321-321-321",
				PartitionUUID:   "123-123-123",
			},
		},
	}
	restore := secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-123-123", nil
	})
	defer restore()
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		return []string{}, nil
	})()
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte,
		options *sb.ActivateVolumeOptions) error {
		c.Check(options, DeepEquals, &sb.ActivateVolumeOptions{})
		c.Check(key, DeepEquals, []byte("fooo"))
		c.Check(volumeName, Matches, "ubuntu-save-random-uuid-123-123")
		c.Check(sourceDevicePath, Equals, "/dev/disk/by-uuid/321-321-321")
		return nil
	})
	defer restore()
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingProtectorKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, IsNil)
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		PartDevice:   "/dev/disk/by-uuid/321-321-321",
		FsDevice:     "/dev/mapper/ubuntu-save-random-uuid-123-123",
		IsEncrypted:  true,
		UnlockMethod: secboot.UnlockedWithKey,
	})
}

type fakeKeyDataReader struct {
	name string
	*bytes.Reader
}

func newFakeKeyDataReader(name string, data []byte) *fakeKeyDataReader {
	return &fakeKeyDataReader{
		name:   name,
		Reader: bytes.NewReader(data),
	}
}

func (f *fakeKeyDataReader) ReadableName() string {
	return f.name
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingProtectorKeyHappy(c *C) {
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-save-enc",
				FilesystemUUID:  "321-321-321",
				PartitionUUID:   "123-123-123",
			},
		},
	}
	restore := secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-123-123", nil
	})
	defer restore()
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		return []string{"some-key-without-data", "some-other-key", "the-plainkey"}, nil
	})()
	defer secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		switch slot {
		case "the-plainkey":
			return newFakeKeyDataReader(slot, []byte(`{"platform_name": "plainkey"}`)), nil
		case "some-key-without-data":
			return nil, fmt.Errorf("there is no key data")
		case "some-other-key":
			return newFakeKeyDataReader(slot, []byte(`{"platform_name": "foo"}`)), nil
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})()

	setProtectorKeysCalls := 0
	secboot.MockSetProtectorKeys(func(keys ...[]byte) {
		setProtectorKeysCalls++
		switch setProtectorKeysCalls {
		case 1:
			c.Check(keys, HasLen, 1)
			c.Check(keys[0], DeepEquals, []byte("fooo"))
		case 2:
			c.Check(keys, HasLen, 0)
		default:
			c.Errorf("unexpected call")
		}
	})

	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName string, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		c.Check(setProtectorKeysCalls, Equals, 1)
		c.Check(options, DeepEquals, &sb.ActivateVolumeOptions{KeyringPrefix: "ubuntu-fde"})
		c.Check(volumeName, Matches, "ubuntu-save-random-uuid-123-123")
		c.Check(sourceDevicePath, Equals, "/dev/disk/by-uuid/321-321-321")
		return nil
	})
	defer restore()
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte,
		options *sb.ActivateVolumeOptions) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingProtectorKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, IsNil)
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		PartDevice:   "/dev/disk/by-uuid/321-321-321",
		FsDevice:     "/dev/mapper/ubuntu-save-random-uuid-123-123",
		IsEncrypted:  true,
		UnlockMethod: secboot.UnlockedWithKey,
	})
	c.Check(setProtectorKeysCalls, Equals, 2)
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingProtectorKeyErr(c *C) {
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-save-enc",
				FilesystemUUID:  "321-321-321",
				PartitionUUID:   "123-123-123",
			},
		},
	}
	restore := secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-123-123", nil
	})
	defer restore()
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		return []string{}, nil
	})()
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte,
		options *sb.ActivateVolumeOptions) error {
		return fmt.Errorf("failed")
	})
	defer restore()
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingProtectorKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, ErrorMatches, "failed")
	// we would have at least identified that the device is a decrypted one
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		IsEncrypted: true,
		PartDevice:  "/dev/disk/by-uuid/321-321-321",
		FsDevice:    "",
	})
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyErr(c *C) {
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		return nil, fmt.Errorf(`cannot run ["fde-reveal-key"]: helper error`)
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}
	defaultDevice := "name"
	mockSealedKeyFile := makeMockSealedKeyFile(c, nil)

	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		c.Assert(keys, HasLen, 1)
		keyData := keys[0]
		_, _, err := keyData.RecoverKeys()
		if err != nil {
			return err
		}
		c.Fatal("should not get this far")
		return nil
	})
	defer restore()

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		WhichModel: func() (*asserts.Model, error) {
			return fakeModel, nil
		},
	}
	_, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)
	c.Assert(err, ErrorMatches, `cannot activate encrypted device "/dev/disk/by-uuid/enc-dev-uuid": cannot perform action because of an unexpected error: cannot run \["fde-reveal-key"\]: helper error`)
}

func (s *secbootSuite) TestLockSealedKeysCallsFdeReveal(c *C) {
	var ops []string
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		ops = append(ops, req.Op)
		return nil, nil
	})
	defer restore()
	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	err := secboot.LockSealedKeys()
	c.Assert(err, IsNil)

	c.Check(ops, DeepEquals, []string{"lock"})
}

func (s *secbootSuite) testSealKeysWithFDESetupHookHappy(c *C, useKeyFiles bool) {
	n := 0
	sealedPrefix := []byte("SEALED:")
	rawHandle1 := json.RawMessage(`{"handle-for":"key1"}`)
	var runFDESetupHookReqs []*fde.SetupRequest
	runFDESetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		n++
		runFDESetupHookReqs = append(runFDESetupHookReqs, req)
		payload := append(sealedPrefix, req.Key...)
		var handle *json.RawMessage
		if req.KeyName == "key1" {
			handle = &rawHandle1
		}
		res := &fde.InitialSetupResult{
			EncryptedKey: payload,
			Handle:       handle,
		}
		return json.Marshal(res)
	}

	tmpDir := c.MkDir()
	auxKeyFn := filepath.Join(tmpDir, "aux-key")
	params := secboot.SealKeysWithFDESetupHookParams{
		Model:      fakeModel,
		AuxKeyFile: auxKeyFn,
	}
	containerA := secboot.CreateMockBootstrappedContainer()
	containerB := secboot.CreateMockBootstrappedContainer()
	myKeys := []secboot.SealKeyRequest{
		{BootstrappedContainer: containerA, KeyName: "key1", SlotName: "foo1"},
		{BootstrappedContainer: containerB, KeyName: "key2", SlotName: "foo2"},
	}
	if useKeyFiles {
		myKeys[0].KeyFile = filepath.Join(tmpDir, "key-file-1")
		myKeys[1].KeyFile = filepath.Join(tmpDir, "key-file-2")
	}
	err := secboot.SealKeysWithFDESetupHook(runFDESetupHook, myKeys, &params)
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	c.Check(runFDESetupHookReqs, HasLen, 2)
	c.Check(runFDESetupHookReqs[0].Op, Equals, "initial-setup")
	c.Check(runFDESetupHookReqs[1].Op, Equals, "initial-setup")
	c.Check(runFDESetupHookReqs[0].KeyName, Equals, "key1")
	c.Check(runFDESetupHookReqs[1].KeyName, Equals, "key2")
	_, aHasSlot := containerA.Slots["foo1"]
	c.Check(aHasSlot, Equals, true)
	_, bHasSlot := containerB.Slots["foo2"]
	c.Check(bHasSlot, Equals, true)
	if useKeyFiles {
		c.Check(containerA.Tokens, HasLen, 0)
		c.Check(containerB.Tokens, HasLen, 0)
		c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-1")), Equals, true)
		c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-2")), Equals, true)
	} else {
		c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-1")), Equals, false)
		c.Check(osutil.FileExists(filepath.Join(tmpDir, "key-file-2")), Equals, false)
		_, aHasToken := containerA.Tokens["foo1"]
		c.Check(aHasToken, Equals, true)
		_, bHasToken := containerB.Tokens["foo2"]
		c.Check(bHasToken, Equals, true)
	}
}

func (s *secbootSuite) TestSealKeysWithFDESetupHookHappyKeyFiles(c *C) {
	const useKeyFiles = true
	s.testSealKeysWithFDESetupHookHappy(c, useKeyFiles)
}

func (s *secbootSuite) TestSealKeysWithFDESetupHookHappyTokens(c *C) {
	const useKeyFiles = false
	s.testSealKeysWithFDESetupHookHappy(c, useKeyFiles)
}

func makeMockDiskKey() keys.EncryptionKey {
	return keys.EncryptionKey{0, 1, 2, 3, 4, 5}
}

func makeMockAuxKey() keys.AuxKey {
	return keys.AuxKey{6, 7, 8, 9}
}

func makeMockUnencryptedPayload() []byte {
	diskKey := makeMockDiskKey()
	auxKey := makeMockAuxKey()
	payload := new(bytes.Buffer)
	binary.Write(payload, binary.BigEndian, uint16(len(diskKey)))
	payload.Write(diskKey)
	binary.Write(payload, binary.BigEndian, uint16(len(auxKey[:])))
	payload.Write(auxKey[:])
	return payload.Bytes()
}

func makeMockEncryptedPayload() []byte {
	pl := makeMockUnencryptedPayload()
	// rot13 ftw
	for i := range pl {
		pl[i] = pl[i] ^ 0x13
	}
	return pl
}

func makeMockEncryptedPayloadString() string {
	return base64.StdEncoding.EncodeToString(makeMockEncryptedPayload())
}

func makeMockSealedKeyFile(c *C, handle json.RawMessage) string {
	mockSealedKeyFile := filepath.Join(c.MkDir(), "keyfile")
	var handleJSON string
	if len(handle) != 0 {
		handleJSON = fmt.Sprintf(`"platform_handle":%s,`, handle)
	}
	sealedKeyContent := fmt.Sprintf(`{"platform_name":"fde-hook-v2",%s"encrypted_payload":"%s"}`, handleJSON, makeMockEncryptedPayloadString())
	err := os.WriteFile(mockSealedKeyFile, []byte(sealedKeyContent), 0600)
	c.Assert(err, IsNil)
	return mockSealedKeyFile
}

var fakeModel = assertstest.FakeAssertion(map[string]interface{}{
	"type":         "model",
	"authority-id": "my-brand",
	"series":       "16",
	"brand-id":     "my-brand",
	"model":        "my-model",
	"grade":        "signed",
	"architecture": "amd64",
	"base":         "core20",
	"snaps": []interface{}{
		map[string]interface{}{
			"name":            "pc-kernel",
			"id":              "pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza",
			"type":            "kernel",
			"default-channel": "20",
		},
		map[string]interface{}{
			"name":            "pc",
			"id":              "UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH",
			"type":            "gadget",
			"default-channel": "20",
		}},
}).(*asserts.Model)

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV2(c *C) {
	var reqs []*fde.RevealKeyRequest
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		reqs = append(reqs, req)
		return []byte(fmt.Sprintf(`{"key": "%s"}`, base64.StdEncoding.EncodeToString(makeMockUnencryptedPayload()))), nil
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-for-test", nil
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	expectedKey := makeMockDiskKey()
	expectedAuxKey := makeMockAuxKey()
	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		c.Check(s.currentModel.Model(), Equals, fakeModel.Model())
		c.Assert(keys, HasLen, 1)
		keyData := keys[0]

		activated++
		c.Check(options.RecoveryKeyTries, Equals, 0)
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		key, auxKey, err := keyData.RecoverKeys()
		c.Assert(err, IsNil)
		c.Check([]byte(key), DeepEquals, []byte(expectedKey))
		c.Check([]byte(auxKey), DeepEquals, expectedAuxKey[:])
		return nil
	})
	defer restore()

	defaultDevice := "device-name"
	handle := json.RawMessage(`{"a": "handle"}`)
	mockSealedKeyFile := makeMockSealedKeyFile(c, handle)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		WhichModel: func() (*asserts.Model, error) {
			return fakeModel, nil
		},
	}
	res, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		UnlockMethod: secboot.UnlockedWithSealedKey,
		IsEncrypted:  true,
		PartDevice:   "/dev/disk/by-partuuid/enc-dev-partuuid",
		FsDevice:     "/dev/mapper/device-name-random-uuid-for-test",
	})
	c.Check(activated, Equals, 1)
	c.Check(reqs, HasLen, 1)
	c.Check(reqs[0].Op, Equals, "reveal")
	c.Check(reqs[0].SealedKey, DeepEquals, makeMockEncryptedPayload())
	c.Check(reqs[0].Handle, DeepEquals, &handle)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV2ActivationError(c *C) {
	restore := secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-for-test", nil
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		activated++
		return fmt.Errorf("some activation error")
	})
	defer restore()

	defaultDevice := "device-name"
	handle := json.RawMessage(`{"a": "handle"}`)
	mockSealedKeyFile := makeMockSealedKeyFile(c, handle)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		WhichModel: func() (*asserts.Model, error) {
			return fakeModel, nil
		},
	}
	res, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)
	c.Assert(err, ErrorMatches, `cannot activate encrypted device "/dev/disk/by-uuid/enc-dev-uuid": some activation error`)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		IsEncrypted: true,
		PartDevice:  "/dev/disk/by-partuuid/enc-dev-partuuid",
	})
	c.Check(activated, Equals, 1)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV2AllowRecoverKey(c *C) {
	var reqs []*fde.RevealKeyRequest
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		reqs = append(reqs, req)
		return []byte("invalid-json"), nil
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-for-test", nil
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		c.Assert(keys, HasLen, 1)
		keyData := keys[0]

		activated++
		c.Check(options.RecoveryKeyTries, Equals, 3)
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		_, _, err := keyData.RecoverKeys()
		c.Assert(err, NotNil)
		return sb.ErrRecoveryKeyUsed
	})
	defer restore()

	defaultDevice := "device-name"
	handle := json.RawMessage(`{"a": "handle"}`)
	mockSealedKeyFile := makeMockSealedKeyFile(c, handle)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		AllowRecoveryKey: true,
		WhichModel: func() (*asserts.Model, error) {
			return fakeModel, nil
		},
	}
	res, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		UnlockMethod: secboot.UnlockedWithRecoveryKey,
		IsEncrypted:  true,
		PartDevice:   "/dev/disk/by-partuuid/enc-dev-partuuid",
		FsDevice:     "/dev/mapper/device-name-random-uuid-for-test",
	})
	c.Check(activated, Equals, 1)
	c.Check(reqs, HasLen, 1)
	c.Check(reqs[0].Op, Equals, "reveal")
	c.Check(reqs[0].SealedKey, DeepEquals, makeMockEncryptedPayload())
	c.Check(reqs[0].Handle, DeepEquals, &handle)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV1(c *C) {
	mockDiskKey := []byte("unsealed-key--64-chars-long-and-not-json-to-match-denver-project")
	c.Assert(len(mockDiskKey), Equals, 64)

	var reqs []*fde.RevealKeyRequest
	// The v1 hooks will just return raw bytes. This is deprecated but
	// we need to keep compatbility with the v1 implementation because
	// there is a project "denver" that ships with v1 hooks.
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		reqs = append(reqs, req)
		return mockDiskKey, nil
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-for-test", nil
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	mockSealedKey := []byte("USK$foobar")

	restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
		c.Check(keyfile, Equals, "the-key-file")
		c.Check(hintExpectFDEHook, Equals, true)
		kl.LoadedFDEHookKeyV1(mockSealedKey)
		return nil
	})
	defer restore()

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte, options *sb.ActivateVolumeOptions) error {
		activated++
		c.Check(volumeName, Equals, "device-name-random-uuid-for-test")
		c.Check(sourceDevicePath, Equals, "/dev/disk/by-uuid/enc-dev-uuid")
		c.Check(key, DeepEquals, mockDiskKey)
		c.Check(options, DeepEquals, &sb.ActivateVolumeOptions{})
		return nil
	})

	defer restore()

	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		c.Errorf("unexpected calls")
		return fmt.Errorf("unexpected calls")
	})
	defer restore()

	defaultDevice := "device-name"

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{}
	res, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, "the-key-file", opts)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		UnlockMethod: secboot.UnlockedWithSealedKey,
		IsEncrypted:  true,
		PartDevice:   "/dev/disk/by-partuuid/enc-dev-partuuid",
		FsDevice:     "/dev/mapper/device-name-random-uuid-for-test",
	})
	c.Check(activated, Equals, 1)
	c.Assert(reqs, HasLen, 1)
	c.Check(reqs[0].Op, Equals, "reveal")
	c.Check(reqs[0].SealedKey, DeepEquals, mockSealedKey)
	c.Check(reqs[0].Handle, IsNil)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV1FallbackKeyData(c *C) {
	// If there is an old key file but that does not manage to
	// open the disk, we should still try to open using key data
	// because there might be a keyslot that has a key data that
	// works.
	mockDiskKey := []byte("unsealed-key--64-chars-long-and-not-json-to-match-denver-project")
	c.Assert(len(mockDiskKey), Equals, 64)

	var reqs []*fde.RevealKeyRequest
	// The v1 hooks will just return raw bytes. This is deprecated but
	// we need to keep compatbility with the v1 implementation because
	// there is a project "denver" that ships with v1 hooks.
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		reqs = append(reqs, req)
		return mockDiskKey, nil
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-for-test", nil
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	mockSealedKey := []byte("USK$foobar")

	restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
		c.Check(keyfile, Equals, "the-key-file")
		c.Check(hintExpectFDEHook, Equals, true)
		kl.LoadedFDEHookKeyV1(mockSealedKey)
		return nil
	})
	defer restore()

	activatedLegacy := 0
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte, options *sb.ActivateVolumeOptions) error {
		activatedLegacy++
		c.Check(volumeName, Equals, "device-name-random-uuid-for-test")
		c.Check(sourceDevicePath, Equals, "/dev/disk/by-uuid/enc-dev-uuid")
		c.Check(key, DeepEquals, mockDiskKey)
		c.Check(options, DeepEquals, &sb.ActivateVolumeOptions{})
		return fmt.Errorf("that did not work")
	})

	defer restore()

	activatedKeyData := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		activatedKeyData++
		c.Check(volumeName, Equals, "device-name-random-uuid-for-test")
		c.Check(sourceDevicePath, Equals, "/dev/disk/by-uuid/enc-dev-uuid")
		c.Check(authRequestor, NotNil)
		c.Check(options, DeepEquals, &sb.ActivateVolumeOptions{
			PassphraseTries: 1,
			KeyringPrefix:   "ubuntu-fde",
			LegacyDevicePaths: []string{
				"/dev/disk/by-partuuid/enc-dev-partuuid",
			},
		})
		c.Check(keys, HasLen, 0)
		return nil
	})
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	defaultDevice := "device-name"

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{}
	res, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, "the-key-file", opts)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		UnlockMethod: secboot.UnlockedWithSealedKey,
		IsEncrypted:  true,
		PartDevice:   "/dev/disk/by-partuuid/enc-dev-partuuid",
		FsDevice:     "/dev/mapper/device-name-random-uuid-for-test",
	})
	c.Check(activatedLegacy, Equals, 1)
	c.Assert(reqs, HasLen, 1)
	c.Check(reqs[0].Op, Equals, "reveal")
	c.Check(reqs[0].SealedKey, DeepEquals, mockSealedKey)
	c.Check(reqs[0].Handle, IsNil)
	c.Check(activatedKeyData, Equals, 1)
	c.Check(logbuf.String(), testutil.Contains, ` WARNING: attempting opening device /dev/disk/by-uuid/enc-dev-uuid  with key file the-key-file failed: that did not work`)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyBadJSONv2(c *C) {
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		return []byte("invalid-json"), nil
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() (string, error) {
		return "random-uuid-for-test", nil
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				FilesystemUUID:  "enc-dev-uuid",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		c.Assert(keys, HasLen, 1)
		keyData := keys[0]

		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		_, _, err := keyData.RecoverKeys()
		if err != nil {
			return err
		}
		c.Fatal("should not get this far")
		return nil
	})
	defer restore()

	defaultDevice := "device-name"
	mockSealedKeyFile := makeMockSealedKeyFile(c, nil)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		WhichModel: func() (*asserts.Model, error) {
			return fakeModel, nil
		},
	}
	_, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)

	c.Check(err, ErrorMatches, `cannot activate encrypted device \".*\": invalid key data: cannot unmarshal cleartext key payload: EOF`)
}

func (s *secbootSuite) TestMarkSuccessfulNotEncrypted(c *C) {
	restore := secboot.MockSbConnectToDefaultTPM(func() (*sb_tpm2.Connection, error) {
		c.Fatalf("should not get called")
		return nil, errors.New("boom")
	})
	defer restore()

	// device is not encrypted
	encrypted := device.HasEncryptedMarkerUnder(dirs.SnapFDEDir)
	c.Assert(encrypted, Equals, false)

	// mark successful returns no error but does not talk to the TPM
	err := secboot.MarkSuccessful()
	c.Check(err, IsNil)
}

func (s *secbootSuite) TestMarkSuccessfulEncryptedTPM(c *C) {
	s.testMarkSuccessfulEncrypted(c, device.SealingMethodTPM, 1)
}

func (s *secbootSuite) TestMarkSuccessfulEncryptedFDE(c *C) {
	s.testMarkSuccessfulEncrypted(c, device.SealingMethodFDESetupHook, 0)
}

func (s *secbootSuite) testMarkSuccessfulEncrypted(c *C, sealingMethod device.SealingMethod, expectedDaLockResetCalls int) {
	_, restore := mockSbTPMConnection(c, nil)
	defer restore()

	// device is encrypted
	err := os.MkdirAll(dirs.SnapFDEDir, 0700)
	c.Assert(err, IsNil)
	saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDir)
	err = os.MkdirAll(saveFDEDir, 0700)
	c.Assert(err, IsNil)

	err = device.StampSealedKeys(dirs.GlobalRootDir, sealingMethod)
	c.Assert(err, IsNil)

	// write fake lockout auth
	lockoutAuthValue := []byte("tpm-lockout-auth-key")
	err = os.WriteFile(filepath.Join(saveFDEDir, "tpm-lockout-auth"), lockoutAuthValue, 0600)
	c.Assert(err, IsNil)

	daLockResetCalls := 0
	restore = secboot.MockSbTPMDictionaryAttackLockReset(func(tpm *sb_tpm2.Connection, lockContext tpm2.ResourceContext, lockContextAuthSession tpm2.SessionContext, sessions ...tpm2.SessionContext) error {
		daLockResetCalls++
		// Below this code pokes at the private data from
		//   github.com/canonical/go-tpm2/resources.go
		//   type resourceContext struct {
		//     ...
		//     authValue []byte
		//   }
		// there is no exported API to get the auth value. If go-tpm2
		// starts chaning it's probably not worth updating this
		// part of the test and it can just get removed.
		fv := reflect.ValueOf(lockContext).Elem().FieldByName("authValue")
		c.Check(fv.Bytes(), DeepEquals, lockoutAuthValue)
		return nil
	})
	defer restore()

	err = secboot.MarkSuccessful()
	c.Check(err, IsNil)

	c.Check(daLockResetCalls, Equals, expectedDaLockResetCalls)
}

func (s *secbootSuite) TestHookKeyRevealV3(c *C) {
	k := &secboot.KeyRevealerV3{}

	encryptedKey := []byte{1, 2, 3, 4}
	decryptedKey := []byte{5, 6, 7, 8}

	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		c.Check(req.Op, Equals, "reveal")
		c.Check(req.SealedKey, DeepEquals, encryptedKey)
		c.Assert(req.Handle, NotNil)
		c.Check(*req.Handle, DeepEquals, json.RawMessage([]byte("the-handle")))
		return decryptedKey, nil
	})
	defer restore()

	plain, err := k.RevealKey([]byte("the-handle"), encryptedKey, []byte{})
	c.Assert(err, IsNil)
	c.Check(plain, DeepEquals, decryptedKey)
}

func (s *secbootSuite) TestHookKeyRevealV3Error(c *C) {
	k := &secboot.KeyRevealerV3{}

	encryptedKey := []byte{1, 2, 3, 4}

	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		c.Check(req.Op, Equals, "reveal")
		c.Check(req.SealedKey, DeepEquals, encryptedKey)
		c.Assert(req.Handle, NotNil)
		c.Check(*req.Handle, DeepEquals, json.RawMessage([]byte("the-handle")))
		return nil, fmt.Errorf("some error")
	})
	defer restore()

	_, err := k.RevealKey([]byte("the-handle"), encryptedKey, []byte{})
	c.Assert(err, ErrorMatches, `some error`)
}

func (s *secbootSuite) TestAddBootstrapKeyOnExistingDisk(c *C) {
	defer secboot.MockGetDiskUnlockKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(devicePath, Equals, "/dev/foo")
		c.Check(remove, Equals, false)
		return []byte{1, 2, 3, 4}, nil
	})()

	defer secboot.MockAddLUKS2ContainerUnlockKey(func(devicePath string, keyslotName string, existingKey sb.DiskUnlockKey, newKey sb.DiskUnlockKey) error {
		c.Check(devicePath, Equals, "/dev/foo")
		c.Check(keyslotName, Equals, "bootstrap-key")
		c.Check(existingKey, DeepEquals, sb.DiskUnlockKey([]byte{1, 2, 3, 4}))
		c.Check(newKey, DeepEquals, sb.DiskUnlockKey([]byte{5, 6, 7, 8}))
		return nil
	})()

	err := secboot.AddBootstrapKeyOnExistingDisk("/dev/foo", []byte{5, 6, 7, 8})
	c.Check(err, IsNil)
}

func (s *secbootSuite) TestAddBootstrapKeyOnExistingDiskKeyringError(c *C) {
	defer secboot.MockGetDiskUnlockKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(devicePath, Equals, "/dev/foo")
		c.Check(remove, Equals, false)
		return nil, fmt.Errorf("some error")
	})()

	defer secboot.MockAddLUKS2ContainerUnlockKey(func(devicePath string, keyslotName string, existingKey sb.DiskUnlockKey, newKey sb.DiskUnlockKey) error {
		c.Errorf("unexpected call")
		return nil
	})()

	err := secboot.AddBootstrapKeyOnExistingDisk("/dev/foo", []byte{5, 6, 7, 8})
	c.Check(err, ErrorMatches, `cannot get key for unlocked disk /dev/foo: some error`)
}

func (s *secbootSuite) TestAddBootstrapKeyOnExistingDiskLUKS2Error(c *C) {
	defer secboot.MockGetDiskUnlockKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.DiskUnlockKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(devicePath, Equals, "/dev/foo")
		c.Check(remove, Equals, false)
		return []byte{1, 2, 3, 4}, nil
	})()

	defer secboot.MockAddLUKS2ContainerUnlockKey(func(devicePath string, keyslotName string, existingKey sb.DiskUnlockKey, newKey sb.DiskUnlockKey) error {
		c.Check(devicePath, Equals, "/dev/foo")
		c.Check(keyslotName, Equals, "bootstrap-key")
		c.Check(existingKey, DeepEquals, sb.DiskUnlockKey([]byte{1, 2, 3, 4}))
		c.Check(newKey, DeepEquals, sb.DiskUnlockKey([]byte{5, 6, 7, 8}))
		return fmt.Errorf("some error")
	})()

	err := secboot.AddBootstrapKeyOnExistingDisk("/dev/foo", []byte{5, 6, 7, 8})
	c.Check(err, ErrorMatches, `cannot enroll new installation key: some error`)
}

func (s *secbootSuite) TestRenameKeys(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	expectedRenames := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
	}

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		newName, expected := expectedRenames[slotName]
		c.Check(expected, Equals, true)
		c.Check(renameTo, Equals, newName)
		delete(expectedRenames, slotName)
		return nil
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, IsNil)

	c.Check(expectedRenames, HasLen, 0)
}

func (s *secbootSuite) TestRenameKeysBadInput(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Errorf("unexpected call")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Errorf("unexpected call")
		return nil
	})()

	toRename := map[string]string{
		"slot-a": "slot-b",
		"slot-b": "slot-c",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `internal error: keyslot name slot-b used as source and target of a rename`)
}

func (s *secbootSuite) TestRenameKeysNameExists(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Errorf("unexpected call")
		return nil
	})()

	toRename := map[string]string{
		"slot-a": "slot-b",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `slot name slot-b is already in use`)
}

func (s *secbootSuite) TestRenameKeysNoRename(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	expectedRenames := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
	}

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		return sb.ErrMissingCryptsetupFeature
	})()

	defer secboot.MockCopyAndRemoveLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		expectedRename, expected := expectedRenames[slotName]
		c.Assert(expected, Equals, true)
		c.Check(renameTo, Equals, expectedRename)
		delete(expectedRenames, slotName)
		return nil
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, IsNil)

	c.Check(expectedRenames, HasLen, 0)
}

func (s *secbootSuite) TestRenameKeysListError(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return nil, fmt.Errorf("some error")
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Errorf("unexpected call")
		return nil
	})()

	defer secboot.MockDeleteLUKS2ContainerKey(func(devicePath, slotName string) error {
		c.Errorf("unexpected call")
		return nil
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `cannot list slots in partition save partition: some error`)
}

func (s *secbootSuite) TestRenameKeysRenameError(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		return fmt.Errorf("some other error")
	})()

	defer secboot.MockCopyAndRemoveLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Errorf("unexpected call")
		return nil
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `cannot rename container key: some other error`)
}

func (s *secbootSuite) TestRenameKeysDeleteError(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		return sb.ErrMissingCryptsetupFeature
	})()

	defer secboot.MockCopyAndRemoveLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		return fmt.Errorf("some error")
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `cannot rename old container key: some error`)
}

func (s *secbootSuite) TestDeleteKeys(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	expectedRemovals := map[string]bool{
		"slot-b": true,
		"slot-c": true,
	}

	defer secboot.MockDeleteLUKS2ContainerKey(func(devicePath, slotName string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		_, expected := expectedRemovals[slotName]
		c.Check(expected, Equals, true)
		delete(expectedRemovals, slotName)
		return nil
	})()

	toRemove := map[string]bool{
		"slot-b": true,
		"slot-c": true,
		"slot-d": true,
	}

	err := secboot.DeleteKeys("/dev/foo", toRemove)
	c.Assert(err, IsNil)

	c.Check(expectedRemovals, HasLen, 0)
}

func (s *secbootSuite) TestDeleteKeysErrorList(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return nil, fmt.Errorf("some error")
	})()

	defer secboot.MockDeleteLUKS2ContainerKey(func(devicePath, slotName string) error {
		c.Errorf("unexpected call")
		return nil
	})()

	toRemove := map[string]bool{
		"slot-b": true,
		"slot-c": true,
		"slot-d": true,
	}

	err := secboot.DeleteKeys("/dev/foo", toRemove)
	c.Assert(err, ErrorMatches, `cannot list slots in partition save partition: some error`)
}

func (s *secbootSuite) TestDeleteKeysErrorDelete(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockDeleteLUKS2ContainerKey(func(devicePath, slotName string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		return fmt.Errorf("some error")
	})()

	toRemove := map[string]bool{
		"slot-b": true,
		"slot-c": true,
		"slot-d": true,
	}

	err := secboot.DeleteKeys("/dev/foo", toRemove)
	c.Assert(err, ErrorMatches, `cannot remove old container key: some error`)
}

type SomeStructure struct {
	TPM2PCRProfile secboot.SerializedPCRProfile `json:"tpm2-pcr-profile"`
}

func (s *secbootSuite) TestSerializedProfile(c *C) {
	krp := SomeStructure{
		TPM2PCRProfile: secboot.SerializedPCRProfile("serialized-profile"),
	}

	data, err := json.Marshal(krp)
	c.Assert(err, IsNil)
	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	c.Assert(err, IsNil)
	c.Check(raw, DeepEquals, map[string]any{
		// binary blobk is serialized to base64 by default
		"tpm2-pcr-profile": base64.StdEncoding.EncodeToString([]byte("serialized-profile")),
	})
}

type testModel struct {
	name string
}

func (tm *testModel) Series() string {
	return "16"
}
func (tm *testModel) BrandID() string {
	return "some-brand"
}
func (tm *testModel) Model() string {
	return tm.name
}
func (tm *testModel) Classic() bool {
	return false
}
func (tm *testModel) Grade() asserts.ModelGrade {
	return asserts.ModelSecured
}
func (tm *testModel) SignKeyID() string {
	return "some-key"
}

func (s *secbootSuite) TestResealKeysWithFDESetupHookV1(c *C) {
	key1 := []byte(`USK$blahblahblah`)

	tmpdir := c.MkDir()
	key1Fn := filepath.Join(tmpdir, "key1.key")
	err := os.WriteFile(key1Fn, key1, 0644)
	c.Assert(err, IsNil)

	m := &testModel{
		name: "mytest",
	}

	key1Location := secboot.KeyDataLocation{
		DevicePath: "/dev/foo",
		SlotName:   "default",
		KeyFile:    key1Fn,
	}

	primaryKeyGetter := func() ([]byte, error) {
		c.Errorf("unexpected call")
		return nil, fmt.Errorf("unexpected call")
	}
	err = secboot.ResealKeysWithFDESetupHook([]secboot.KeyDataLocation{key1Location}, primaryKeyGetter, []secboot.ModelForSealing{m}, []string{"run"})
	c.Assert(err, IsNil)

	// Nothing should have happened. But we make sure that they key is still there untouched.

	key, err := os.ReadFile(key1Fn)
	c.Assert(err, IsNil)
	c.Check(key, DeepEquals, key1)
}

func (s *secbootSuite) TestResealKeysWithFDESetupHookV2(c *C) {
	auxKey := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	key1 := []byte(`{"platform_name":"fde-hook-v2","platform_handle":{"handle-for":"key1"},"encrypted_payload":"U0VBTEVEOgAEAQIDBAAgAQIDBAUGBwgJCgsMDQ4PEAECAwQFBgcICQoLDA0ODxA=","authorized_snap_models":{"alg":"sha256","key_digest":"KTj3yfwaA090S9iS3TuTqEdU8+taRAUy/PVbJAhoqpI=","hmacs":["O8n/7j4ZT12hBjbF4rzPHpSUna69e7I43a90oZaB3HY="]}}`)

	tmpdir := c.MkDir()
	key1Fn := filepath.Join(tmpdir, "key1.key")
	err := os.WriteFile(key1Fn, key1, 0644)
	c.Assert(err, IsNil)

	m := &testModel{
		name: "mytest",
	}

	beforeReader, err := sb.NewFileKeyDataReader(key1Fn)
	c.Assert(err, IsNil)
	beforeKeyData, err := sb.ReadKeyData(beforeReader)
	c.Assert(err, IsNil)

	beforeAuthorized, err := beforeKeyData.IsSnapModelAuthorized(auxKey, m)
	c.Assert(err, IsNil)
	c.Check(beforeAuthorized, Equals, false)

	key1Location := secboot.KeyDataLocation{
		DevicePath: "/dev/foo",
		SlotName:   "default",
		KeyFile:    key1Fn,
	}

	err = secboot.ResealKeysWithFDESetupHook([]secboot.KeyDataLocation{key1Location}, func() ([]byte, error) { return auxKey, nil }, []secboot.ModelForSealing{m}, []string{"run"})
	c.Assert(err, IsNil)

	afterReader, err := sb.NewFileKeyDataReader(key1Fn)
	c.Assert(err, IsNil)
	afterKeyData, err := sb.ReadKeyData(afterReader)
	c.Assert(err, IsNil)

	afterAuthorized, err := afterKeyData.IsSnapModelAuthorized(auxKey, m)
	c.Assert(err, IsNil)
	c.Check(afterAuthorized, Equals, true)
}

type fakeKeyProtector struct{}

func (fakeKeyProtector) ProtectKey(rand io.Reader, cleartext, aad []byte) (ciphertext []byte, handle []byte, err error) {
	return cleartext, []byte(`"foo"`), nil
}

func (s *secbootSuite) TestResealKeysWithFDESetupHook(c *C) {
	tmpdir := c.MkDir()
	key1Fn := filepath.Join(tmpdir, "key1.key")

	oldModel := &testModel{
		name: "oldmodel",
	}

	newModel := &testModel{
		name: "newmodel",
	}

	primaryKey := []byte{9, 10, 11, 12}

	params := &sb_hooks.KeyParams{
		PrimaryKey: primaryKey,
		Role:       "key1",
		AuthorizedSnapModels: []sb.SnapModel{
			oldModel,
		},
	}

	sb_hooks.SetKeyProtector(&fakeKeyProtector{}, sb_hooks.KeyProtectorNoAEAD)
	protectedKey, _, _, err := sb_hooks.NewProtectedKey(rand.Reader, params)
	c.Assert(err, IsNil)
	sb_hooks.SetKeyProtector(nil, 0)

	restore := secboot.MockReadKeyToken(func(devicePath, slotName string) (*sb.KeyData, error) {
		c.Check(devicePath, Equals, "/dev/somedevice")
		c.Check(slotName, Equals, "token-name")
		return protectedKey, nil
	})
	defer restore()

	keyDataWriter := &myKeyDataWriter{}
	tokenWritten := 0
	restore = secboot.MockNewLUKS2KeyDataWriter(func(devicePath string, name string) (secboot.KeyDataWriter, error) {
		tokenWritten++
		c.Check(devicePath, Equals, "/dev/somedevice")
		c.Check(name, Equals, "token-name")
		return keyDataWriter, nil
	})
	defer restore()

	modelSet := 0
	secboot.MockSetAuthorizedSnapModelsOnHooksKeydata(func(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, models ...sb.SnapModel) error {
		modelSet++
		c.Check([]byte(key), DeepEquals, primaryKey)
		c.Assert(models, HasLen, 1)
		c.Check(models[0].Model(), Equals, "newmodel")
		return nil
	})

	bootModesSet := 0
	secboot.MockSetAuthorizedBootModesOnHooksKeydata(func(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, bootModes ...string) error {
		bootModesSet++
		c.Check([]byte(key), DeepEquals, primaryKey)
		c.Check(bootModes, DeepEquals, []string{"some-mode"})
		return nil
	})

	key1Location := secboot.KeyDataLocation{
		DevicePath: "/dev/somedevice",
		SlotName:   "token-name",
		KeyFile:    key1Fn,
	}

	err = secboot.ResealKeysWithFDESetupHook([]secboot.KeyDataLocation{key1Location}, func() ([]byte, error) { return primaryKey, nil }, []secboot.ModelForSealing{newModel}, []string{"some-mode"})
	c.Assert(err, IsNil)
	c.Check(modelSet, Equals, 1)
	c.Check(bootModesSet, Equals, 1)
	c.Check(tokenWritten, Equals, 1)
}

func (s *secbootSuite) TestResealKeysWithFDESetupHookFromFile(c *C) {
	tmpdir := c.MkDir()
	key1Fn := filepath.Join(tmpdir, "key1.key")

	oldModel := &testModel{
		name: "oldmodel",
	}

	newModel := &testModel{
		name: "newmodel",
	}

	primaryKey := []byte{9, 10, 11, 12}

	params := &sb_hooks.KeyParams{
		PrimaryKey: primaryKey,
		Role:       "key1",
		AuthorizedSnapModels: []sb.SnapModel{
			oldModel,
		},
	}

	sb_hooks.SetKeyProtector(&fakeKeyProtector{}, sb_hooks.KeyProtectorNoAEAD)
	protectedKey, _, _, err := sb_hooks.NewProtectedKey(rand.Reader, params)
	c.Assert(err, IsNil)
	sb_hooks.SetKeyProtector(nil, 0)

	writer := sb.NewFileKeyDataWriter(key1Fn)
	c.Assert(writer, NotNil)
	err = protectedKey.WriteAtomic(writer)
	c.Assert(err, IsNil)

	readKeyTokenCalls := 0
	restore := secboot.MockReadKeyToken(func(devicePath, slotName string) (*sb.KeyData, error) {
		readKeyTokenCalls++
		c.Check(devicePath, Equals, "/dev/foo")
		c.Check(slotName, Equals, "default")
		return nil, fmt.Errorf("some error")
	})
	defer restore()

	modelSet := 0
	secboot.MockSetAuthorizedSnapModelsOnHooksKeydata(func(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, models ...sb.SnapModel) error {
		modelSet++
		c.Check([]byte(key), DeepEquals, primaryKey)
		c.Assert(models, HasLen, 1)
		c.Check(models[0].Model(), Equals, "newmodel")
		return nil
	})

	bootModesSet := 0
	secboot.MockSetAuthorizedBootModesOnHooksKeydata(func(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, bootModes ...string) error {
		bootModesSet++
		c.Check([]byte(key), DeepEquals, primaryKey)
		c.Check(bootModes, DeepEquals, []string{"some-mode"})
		return nil
	})

	key1Location := secboot.KeyDataLocation{
		DevicePath: "/dev/foo",
		SlotName:   "default",
		KeyFile:    key1Fn,
	}

	err = secboot.ResealKeysWithFDESetupHook([]secboot.KeyDataLocation{key1Location}, func() ([]byte, error) { return primaryKey, nil }, []secboot.ModelForSealing{newModel}, []string{"some-mode"})
	c.Assert(err, IsNil)
	c.Check(modelSet, Equals, 1)
	c.Check(bootModesSet, Equals, 1)
	// We tried to read key data from token
	c.Check(readKeyTokenCalls, Equals, 1)
}

func (s *secbootSuite) TestReadKeyFileKeyData(c *C) {
	keyLoader := &secboot.DefaultKeyLoader{}
	const fdeHookHint = false
	tmpDir := c.MkDir()
	keyPath := filepath.Join(tmpDir, "key")
	// KeyData is a json
	err := os.WriteFile(keyPath, []byte(`{}`), 0644)
	c.Assert(err, IsNil)

	newFileKeyDataReaderCalls := 0
	restore := secboot.MockSbNewFileKeyDataReader(func(kf string) (*sb.FileKeyDataReader, error) {
		newFileKeyDataReaderCalls++
		c.Check(kf, Equals, keyPath)
		return sb.NewFileKeyDataReader(kf)
	})
	defer restore()

	readKeyDataCalls := 0
	restore = secboot.MockSbReadKeyData(func(reader sb.KeyDataReader) (*sb.KeyData, error) {
		readKeyDataCalls++
		return sb.ReadKeyData(reader)
	})
	defer restore()

	err = secboot.ReadKeyFile(keyPath, keyLoader, fdeHookHint)
	c.Assert(err, IsNil)
	c.Check(newFileKeyDataReaderCalls, Equals, 1)
	c.Check(readKeyDataCalls, Equals, 1)
	c.Check(keyLoader.KeyData, NotNil)
	c.Check(keyLoader.SealedKeyObject, IsNil)
	c.Check(keyLoader.FDEHookKeyV1, IsNil)
}

func (s *secbootSuite) TestReadKeyFileSealedObject(c *C) {
	keyLoader := &secboot.DefaultKeyLoader{}
	const fdeHookHint = false
	keyPath := filepath.Join("test-data", "keyfile")

	readSealedKeyObjectFromFileCalls := 0
	restore := secboot.MockSbReadSealedKeyObjectFromFile(func(path string) (*sb_tpm2.SealedKeyObject, error) {
		readSealedKeyObjectFromFileCalls++
		c.Check(path, Equals, keyPath)
		return sb_tpm2.ReadSealedKeyObjectFromFile(path)
	})
	defer restore()

	newKeyDataFromSealedKeyObjectFile := 0
	restore = secboot.MockSbNewKeyDataFromSealedKeyObjectFile(func(path string) (*sb.KeyData, error) {
		newKeyDataFromSealedKeyObjectFile++
		c.Check(path, Equals, keyPath)
		return sb_tpm2.NewKeyDataFromSealedKeyObjectFile(path)
	})
	defer restore()

	err := secboot.ReadKeyFile(keyPath, keyLoader, fdeHookHint)
	c.Assert(err, IsNil)
	c.Check(readSealedKeyObjectFromFileCalls, Equals, 1)
	c.Check(newKeyDataFromSealedKeyObjectFile, Equals, 1)
	c.Check(keyLoader.KeyData, NotNil)
	c.Check(keyLoader.SealedKeyObject, NotNil)
	c.Check(keyLoader.FDEHookKeyV1, IsNil)
}

func (s *secbootSuite) TestReadKeyFileFDEHookV1(c *C) {
	keyLoader := &secboot.DefaultKeyLoader{}
	const fdeHookHint = true
	tmpDir := c.MkDir()
	keyPath := filepath.Join(tmpDir, "key")
	// KeyData starts with USK$
	err := os.WriteFile(keyPath, []byte(`USK$blahblah`), 0644)
	c.Assert(err, IsNil)

	err = secboot.ReadKeyFile(keyPath, keyLoader, fdeHookHint)
	c.Assert(err, IsNil)
	c.Check(keyLoader.KeyData, IsNil)
	c.Check(keyLoader.SealedKeyObject, IsNil)
	c.Check(keyLoader.FDEHookKeyV1, DeepEquals, []byte(`USK$blahblah`))
}

type myFakeSealedKeyData struct {
	handle uint32
}

func (m *myFakeSealedKeyData) PCRPolicyCounterHandle() tpm2.Handle {
	return tpm2.Handle(m.handle)
}

type myFakeSealedKeyObject struct {
	handle uint32
}

func (m *myFakeSealedKeyObject) PCRPolicyCounterHandle() tpm2.Handle {
	return tpm2.Handle(m.handle)
}

type myFakeKeyData struct {
	platformName string
	handle       uint32
}

func (k *myFakeKeyData) GetTPMSealedKeyData() (secboot.MockableSealedKeyData, error) {
	return &myFakeSealedKeyData{handle: k.handle}, nil
}
func (k *myFakeKeyData) PlatformName() string {
	return k.platformName
}

type myNonTPMFakeKeyData struct {
	platformName string
}

func (*myNonTPMFakeKeyData) GetTPMSealedKeyData() (secboot.MockableSealedKeyData, error) {
	return nil, fmt.Errorf("Not TPM data")
}

func (k *myNonTPMFakeKeyData) PlatformName() string {
	return k.platformName
}

func (s *secbootSuite) TestGetPCRHandle(c *C) {
	restore := secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{"some-other-key", "some-key"}, nil
	})
	defer restore()

	restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected")
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Check(device, Equals, "foo")
		switch slot {
		case "some-key":
			return newFakeKeyDataReader(slot, []byte{}), nil
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})
	defer restore()

	restore = secboot.MockReadKeyData(func(reader sb.KeyDataReader) (secboot.MockableKeyData, error) {
		return &myFakeKeyData{platformName: "tpm2", handle: 42}, nil
	})
	defer restore()

	const hintExpectFDEHook = false
	handle, err := secboot.GetPCRHandle("foo", "some-key", "do-not-read", hintExpectFDEHook)
	c.Assert(err, IsNil)
	c.Check(handle, Equals, uint32(42))
}

func (s *secbootSuite) TestGetPCRHandleNoKeyslotKeyfile(c *C) {
	restore := secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{}, nil
	})
	defer restore()

	restore = secboot.MockMockableReadKeyFile(func(keyFile string, kl *secboot.MockableKeyLoader, hintExpectFDEHook bool) error {
		c.Check(keyFile, Equals, "read-this-file")
		kl.KeyData = &myFakeKeyData{platformName: "tpm2", handle: 42}
		return nil
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Errorf("unexpected call")
		return nil, fmt.Errorf("unexpected")
	})
	defer restore()

	const hintExpectFDEHook = false
	handle, err := secboot.GetPCRHandle("foo", "some-key", "read-this-file", hintExpectFDEHook)
	c.Assert(err, IsNil)
	c.Check(handle, Equals, uint32(42))
}

func (s *secbootSuite) TestGetPCRHandleKeyslotNoKeyDataKeyfile(c *C) {
	restore := secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{"some-other-key", "some-key"}, nil
	})
	defer restore()

	restore = secboot.MockMockableReadKeyFile(func(keyFile string, kl *secboot.MockableKeyLoader, hintExpectFDEHook bool) error {
		c.Check(keyFile, Equals, "read-this-file")
		kl.KeyData = &myFakeKeyData{platformName: "tpm2", handle: 42}
		return nil
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Check(device, Equals, "foo")
		switch slot {
		case "some-key":
			return nil, fmt.Errorf("some error because data is not here")
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})
	defer restore()

	restore = secboot.MockReadKeyData(func(reader sb.KeyDataReader) (secboot.MockableKeyData, error) {
		c.Errorf("unexpected call")
		return nil, fmt.Errorf("unexpected")
	})
	defer restore()

	const hintExpectFDEHook = false
	handle, err := secboot.GetPCRHandle("foo", "some-key", "read-this-file", hintExpectFDEHook)
	c.Assert(err, IsNil)
	c.Check(handle, Equals, uint32(42))
}

func (s *secbootSuite) TestGetPCRHandleNoKeyslotKeyfileOldFormat(c *C) {
	restore := secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{}, nil
	})
	defer restore()

	restore = secboot.MockMockableReadKeyFile(func(keyFile string, kl *secboot.MockableKeyLoader, hintExpectFDEHook bool) error {
		c.Check(keyFile, Equals, "read-this-file")
		kl.SealedKeyObject = &myFakeSealedKeyObject{handle: 42}
		return nil
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Errorf("unexpected call")
		return nil, fmt.Errorf("unexpected")
	})
	defer restore()

	const hintExpectFDEHook = false
	handle, err := secboot.GetPCRHandle("foo", "some-key", "read-this-file", hintExpectFDEHook)
	c.Assert(err, IsNil)
	c.Check(handle, Equals, uint32(42))
}

func (s *secbootSuite) TestGetPCRHandleHookKeyV1(c *C) {
	restore := secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{}, nil
	})
	defer restore()

	restore = secboot.MockMockableReadKeyFile(func(keyFile string, kl *secboot.MockableKeyLoader, hintExpectFDEHook bool) error {
		c.Check(hintExpectFDEHook, Equals, true)
		c.Check(keyFile, Equals, "read-this-file")
		kl.FDEHookKeyV1 = []byte(`USK$blahblahblah`)
		return nil
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Errorf("unexpected call")
		return nil, fmt.Errorf("unexpected")
	})
	defer restore()

	const hintExpectFDEHook = true
	handle, err := secboot.GetPCRHandle("foo", "some-key", "read-this-file", hintExpectFDEHook)
	c.Assert(err, IsNil)
	c.Check(handle, Equals, uint32(0))
}

func (s *secbootSuite) TestRemoveOldCounterHandles(c *C) {
	_, restore := mockSbTPMConnection(c, nil)
	defer restore()

	restore = secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{"some-other-key", "some-key"}, nil
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Check(device, Equals, "foo")
		switch slot {
		case "some-key":
			return newFakeKeyDataReader(slot, []byte(`tpm2`)), nil
		case "some-other-key":
			return newFakeKeyDataReader(slot, []byte(`other`)), nil
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})
	defer restore()

	restore = secboot.MockReadKeyData(func(reader sb.KeyDataReader) (secboot.MockableKeyData, error) {
		switch reader.ReadableName() {
		case "some-key":
			return &myFakeKeyData{platformName: "tpm2", handle: secboot.PCRPolicyCounterHandleStart + 1}, nil
		case "some-other-key":
			return &myNonTPMFakeKeyData{platformName: "other"}, nil
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})
	defer restore()

	restore = secboot.MockMockableReadKeyFile(func(keyFile string, kl *secboot.MockableKeyLoader, hintExpectFDEHook bool) error {
		switch keyFile {
		case "new-format":
			kl.KeyData = &myFakeKeyData{platformName: "tpm2", handle: secboot.PCRPolicyCounterHandleStart + 2}
		case "old-format":
			kl.SealedKeyObject = &myFakeSealedKeyObject{handle: secboot.AltFallbackObjectPCRPolicyCounterHandle}
		case "just-ignore":
		case "does-not-exist":
			return os.ErrNotExist
		default:
			c.Errorf("unexpected call")
			return fmt.Errorf("unexpected")
		}
		return nil
	})
	defer restore()

	released := make(map[uint32]bool)
	restore = secboot.MockTPMReleaseResources(func(tpm *sb_tpm2.Connection, handle tpm2.Handle) error {
		released[uint32(handle)] = true
		return nil
	})
	defer restore()

	err := secboot.RemoveOldCounterHandles(
		"foo",
		map[string]bool{
			"some-key":       true,
			"some-other-key": true,
		},
		[]string{
			"new-format",
			"old-format",
			"just-ignore",
			"does-not-exist",
		},
		false,
	)

	c.Assert(err, IsNil)
	c.Check(released, DeepEquals, map[uint32]bool{
		secboot.PCRPolicyCounterHandleStart + 1:         true,
		secboot.PCRPolicyCounterHandleStart + 2:         true,
		secboot.AltFallbackObjectPCRPolicyCounterHandle: true,
		secboot.AltRunObjectPCRPolicyCounterHandle:      true,
	})
}

func (s *secbootSuite) TestRemoveOldCounterHandlesFDEHint(c *C) {
	restore := secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "foo")
		return []string{"some-other-key", "some-key"}, nil
	})
	defer restore()

	restore = secboot.MockSbNewLUKS2KeyDataReader(func(device, slot string) (sb.KeyDataReader, error) {
		c.Check(device, Equals, "foo")
		switch slot {
		case "some-key":
			return newFakeKeyDataReader(slot, []byte(`tpm2`)), nil
		case "some-other-key":
			return newFakeKeyDataReader(slot, []byte(`other`)), nil
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})
	defer restore()

	restore = secboot.MockReadKeyData(func(reader sb.KeyDataReader) (secboot.MockableKeyData, error) {
		switch reader.ReadableName() {
		case "some-key":
			return &myFakeKeyData{platformName: "not-tpm2-for-sure", handle: secboot.PCRPolicyCounterHandleStart + 1}, nil
		case "some-other-key":
			return &myNonTPMFakeKeyData{platformName: "other"}, nil
		default:
			c.Errorf("unexpected call")
			return nil, fmt.Errorf("unexpected")
		}
	})
	defer restore()

	restore = secboot.MockMockableReadKeyFile(func(keyFile string, kl *secboot.MockableKeyLoader, hintExpectFDEHook bool) error {
		c.Check(hintExpectFDEHook, Equals, true)
		switch keyFile {
		case "new-format":
			kl.KeyData = &myFakeKeyData{platformName: "not-tpm2-for-sure", handle: secboot.PCRPolicyCounterHandleStart + 2}
		case "just-ignore":
		case "does-not-exist":
			return os.ErrNotExist
		default:
			c.Errorf("unexpected call")
			return fmt.Errorf("unexpected")
		}
		return nil
	})
	defer restore()

	restore = secboot.MockTPMReleaseResources(func(tpm *sb_tpm2.Connection, handle tpm2.Handle) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	err := secboot.RemoveOldCounterHandles(
		"foo",
		map[string]bool{
			"some-key":       true,
			"some-other-key": true,
		},
		[]string{
			"new-format",
			"just-ignore",
			"does-not-exist",
		},
		true,
	)

	c.Assert(err, IsNil)
}

func (s *secbootSuite) TestFindFreeHandle(c *C) {
	_, restore := mockSbTPMConnection(c, nil)
	defer restore()

	restore = secboot.MockTpmGetCapabilityHandles(func(tpm *sb_tpm2.Connection, firstHandle tpm2.Handle, propertyCount uint32, sessions ...tpm2.SessionContext) (handles tpm2.HandleList, err error) {
		c.Check(sessions, HasLen, 0)
		c.Check(uint32(firstHandle), Equals, secboot.PCRPolicyCounterHandleStart)
		c.Check(propertyCount, Equals, secboot.PCRPolicyCounterHandleRange)
		for i := uint32(0); i < secboot.PCRPolicyCounterHandleRange+1; i++ {
			if i != 5 {
				handles = append(handles, tpm2.Handle(secboot.PCRPolicyCounterHandleStart+i))
			}
		}
		return handles, nil
	})
	defer restore()

	handle, err := secboot.FindFreeHandle()
	c.Assert(err, IsNil)
	c.Check(handle, Equals, secboot.PCRPolicyCounterHandleStart+5)
}

func (s *secbootSuite) TestFindFreeHandleNoneFree(c *C) {
	_, restore := mockSbTPMConnection(c, nil)
	defer restore()

	restore = secboot.MockTpmGetCapabilityHandles(func(tpm *sb_tpm2.Connection, firstHandle tpm2.Handle, propertyCount uint32, sessions ...tpm2.SessionContext) (handles tpm2.HandleList, err error) {
		c.Check(sessions, HasLen, 0)
		c.Check(uint32(firstHandle), Equals, secboot.PCRPolicyCounterHandleStart)
		c.Check(propertyCount, Equals, secboot.PCRPolicyCounterHandleRange)
		for i := uint32(0); i < secboot.PCRPolicyCounterHandleRange; i++ {
			handles = append(handles, tpm2.Handle(secboot.PCRPolicyCounterHandleStart+i))
		}
		return handles, nil
	})
	defer restore()

	_, err := secboot.FindFreeHandle()
	c.Assert(err, ErrorMatches, `no free handle on TPM`)
}

func (s *secbootSuite) TestGetPrimaryKeyDigest(c *C) {
	defer secboot.MockDisksDevlinks(func(node string) ([]string, error) {
		c.Errorf("unexpected call")
		return nil, errors.New("unexpected call")
	})()
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(devicePath, Equals, "/dev/test/device")
		c.Check(remove, Equals, false)
		return []byte{0, 1, 2, 3}, nil
	})()
	salt, digest, err := secboot.GetPrimaryKeyDigest("/dev/test/device", crypto.SHA256)
	c.Assert(err, IsNil)
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(devicePath, Equals, "/dev/other/device")
		c.Check(remove, Equals, false)
		return []byte{0, 1, 2, 3}, nil
	})()
	matches, err := secboot.VerifyPrimaryKeyDigest("/dev/other/device", crypto.SHA256, salt, digest)
	c.Assert(err, IsNil)
	c.Check(matches, Equals, true)
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(devicePath, Equals, "/dev/not/matching")
		c.Check(remove, Equals, false)
		return []byte{8, 8, 8, 8}, nil
	})()
	matches, err = secboot.VerifyPrimaryKeyDigest("/dev/not/matching", crypto.SHA256, salt, digest)
	c.Assert(err, IsNil)
	c.Check(matches, Equals, false)
}

func (s *secbootSuite) TestGetPrimaryKeyDigestFallbackDevPath(c *C) {
	defer secboot.MockDisksDevlinks(func(node string) ([]string, error) {
		c.Check(node, Equals, "/dev/test/device")
		return []string{
			"/dev/link/to/ignore",
			"/dev/test/device",
			"/dev/disk/by-partuuid/a9456fe6-9850-41ce-b2ad-cf9b43a34286",
		}, nil
	})()
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(remove, Equals, false)
		if devicePath == "/dev/test/device" {
			return nil, sb.ErrKernelKeyNotFound
		}
		c.Check(devicePath, Equals, "/dev/disk/by-partuuid/a9456fe6-9850-41ce-b2ad-cf9b43a34286")
		return []byte{0, 1, 2, 3}, nil
	})()
	salt, digest, err := secboot.GetPrimaryKeyDigest("/dev/test/device", crypto.SHA256)
	c.Assert(err, IsNil)
	defer secboot.MockDisksDevlinks(func(node string) ([]string, error) {
		c.Check(node, Equals, "/dev/other/device")
		return []string{
			"/dev/link/to/ignore",
			"/dev/other/device",
			"/dev/disk/by-partuuid/58c54e4e-1e86-4bda-a51c-af50ff8447ab",
		}, nil
	})()
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(remove, Equals, false)
		if devicePath == "/dev/other/device" {
			return nil, sb.ErrKernelKeyNotFound
		}
		c.Check(devicePath, Equals, "/dev/disk/by-partuuid/58c54e4e-1e86-4bda-a51c-af50ff8447ab")
		return []byte{0, 1, 2, 3}, nil
	})()
	matches, err := secboot.VerifyPrimaryKeyDigest("/dev/other/device", crypto.SHA256, salt, digest)
	c.Assert(err, IsNil)
	c.Check(matches, Equals, true)
}

func (s *secbootSuite) TestGetPrimaryKey(c *C) {
	defer secboot.MockDisksDevlinks(func(node string) ([]string, error) {
		switch node {
		case "/dev/test/device1":
			return []string{
				"/dev/test/device1",
				"/dev/disk/by-partuuid/a9456fe6-9850-41ce-b2ad-cf9b43a34286",
			}, nil
		case "/dev/test/device2":
			return []string{
				"/dev/test/device2",
				"/dev/disk/by-partuuid/5b081ac5-2432-48a2-b69b-d1bfb7aec6fe",
			}, nil
		default:
			c.Errorf("unexpected call")
			return nil, errors.New("unexpected call")
		}
	})()
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(remove, Equals, false)
		switch devicePath {
		case "/dev/test/device1":
			return nil, sb.ErrKernelKeyNotFound
		case "/dev/disk/by-partuuid/a9456fe6-9850-41ce-b2ad-cf9b43a34286":
			return nil, sb.ErrKernelKeyNotFound
		case "/dev/test/device2":
			return []byte{1, 2, 3, 4}, nil
		default:
			c.Errorf("unexpected call")
			return nil, errors.New("unexpected call")
		}
	})()

	found, err := secboot.GetPrimaryKey([]string{"/dev/test/device1", "/dev/test/device2"}, "/nonexistant")
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, []byte{1, 2, 3, 4})
}

func (s *secbootSuite) TestGetPrimaryKeyFallbackFile(c *C) {
	defer secboot.MockDisksDevlinks(func(node string) ([]string, error) {
		switch node {
		case "/dev/test/device1":
			return []string{
				"/dev/test/device1",
				"/dev/disk/by-partuuid/a9456fe6-9850-41ce-b2ad-cf9b43a34286",
			}, nil
		case "/dev/test/device2":
			return []string{
				"/dev/test/device2",
				"/dev/disk/by-partuuid/5b081ac5-2432-48a2-b69b-d1bfb7aec6fe",
			}, nil
		default:
			c.Errorf("unexpected call")
			return nil, errors.New("unexpected call")
		}
	})()
	defer secboot.MockSbGetPrimaryKeyFromKernel(func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error) {
		c.Check(prefix, Equals, "ubuntu-fde")
		c.Check(remove, Equals, false)
		switch devicePath {
		case "/dev/test/device1":
			return nil, sb.ErrKernelKeyNotFound
		case "/dev/disk/by-partuuid/a9456fe6-9850-41ce-b2ad-cf9b43a34286":
			return nil, sb.ErrKernelKeyNotFound
		case "/dev/test/device2":
			return nil, sb.ErrKernelKeyNotFound
		case "/dev/disk/by-partuuid/5b081ac5-2432-48a2-b69b-d1bfb7aec6fe":
			return nil, sb.ErrKernelKeyNotFound
		default:
			c.Errorf("unexpected call")
			return nil, errors.New("unexpected call")
		}
	})()

	tmpDir := c.MkDir()
	keyFile := filepath.Join(tmpDir, "key-file")
	err := os.WriteFile(keyFile, []byte{1, 2, 3, 4}, 0644)
	c.Assert(err, IsNil)

	found, err := secboot.GetPrimaryKey([]string{"/dev/test/device1", "/dev/test/device2"}, keyFile)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, []byte{1, 2, 3, 4})
}

func (s *secbootSuite) TestReadKeyData(c *C) {
	called := 0
	defer secboot.MockReadKeyToken(func(devicePath, slotName string) (*sb.KeyData, error) {
		called++
		c.Check(devicePath, Equals, "/dev/some-device")
		c.Check(slotName, Equals, "some-slot")
		return &sb.KeyData{}, nil
	})()

	kd, err := secboot.ReadKeyData("/dev/some-device", "some-slot")
	c.Assert(err, IsNil)
	c.Check(kd, NotNil)
	c.Check(called, Equals, 1)

	// it is not possible to mock the internal secboot key data
	c.Check(kd.AuthMode(), Equals, device.AuthModeNone)
	c.Check(kd.PlatformName(), Equals, "")
	c.Check(kd.Role(), Equals, "")
}

func (s *secbootSuite) TestReadKeyDataError(c *C) {
	defer secboot.MockReadKeyToken(func(devicePath, slotName string) (*sb.KeyData, error) {
		return nil, errors.New("boom!")
	})()

	_, err := secboot.ReadKeyData("/dev/some-device", "some-slot")
	c.Assert(err, ErrorMatches, "boom!")
}

func (s *secbootSuite) TestListContainerRecoveryKeyNames(c *C) {
	defer secboot.MockListLUKS2ContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/some-device")
		return []string{"some-slot-1", "some-slot-2"}, nil
	})()

	keyNames, err := secboot.ListContainerRecoveryKeyNames("/dev/some-device")
	c.Assert(err, IsNil)
	c.Check(keyNames, DeepEquals, []string{"some-slot-1", "some-slot-2"})
}

func (s *secbootSuite) TestListContainerUnlockKeyNames(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/some-device")
		return []string{"some-slot-1", "some-slot-2"}, nil
	})()

	keyNames, err := secboot.ListContainerUnlockKeyNames("/dev/some-device")
	c.Assert(err, IsNil)
	c.Check(keyNames, DeepEquals, []string{"some-slot-1", "some-slot-2"})
}
