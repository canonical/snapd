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
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

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
						PassphraseTries:  1,
						RecoveryKeyTries: 3,
						KeyringPrefix:    "ubuntu-fde",
					})
				} else {
					c.Assert(*options, DeepEquals, sb.ActivateVolumeOptions{
						PassphraseTries: 1,
						// activation with recovery key was disabled
						RecoveryKeyTries: 0,
						KeyringPrefix:    "ubuntu-fde",
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

			opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: tc.rkAllow,
				WhichModel: func() (*asserts.Model, error) {
					return fakeModel, nil
				},
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

func (s *secbootSuite) TestSealKey(c *C) {
	mockErr := errors.New("some error")

	for idx, tc := range []struct {
		tpmErr               error
		tpmEnabled           bool
		missingFile          bool
		badSnapFile          bool
		addPCRProfileErr     error
		addSystemdEFIStubErr error
		addSnapModelErr      error
		provisioningErr      error
		sealErr              error
		sealCalls            int
		expectedErr          string
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
			TPMPolicyAuthKeyFile: filepath.Join(tmpDir, "policy-auth-key-file"),

			PCRPolicyCounterHandle: 42,
		}

		myKeys := []secboot.SealKeyRequest{
			{
				BootstrappedContainer: secboot.CreateMockBootstrappedContainer(),
				KeyFile:               filepath.Join(tmpDir, "keyfile"),
			},
			{
				BootstrappedContainer: secboot.CreateMockBootstrappedContainer(),
				KeyFile:               filepath.Join(tmpDir, "keyfile2"),
			},
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
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
		}
		c.Assert(sealCalls, Equals, tc.sealCalls)
	}
}

func (s *secbootSuite) TestResealKey(c *C) {
	mockErr := errors.New("some error")

	for _, tc := range []struct {
		tpmErr                 error
		tpmEnabled             bool
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
	}{
		// happy case
		{tpmEnabled: true, resealCalls: 1, expectedErr: ""},
		// happy case, old keys
		{tpmEnabled: true, resealCalls: 1, revokeCalls: 1, expectedErr: "", oldKeyFiles: true},

		// unhappy cases
		{tpmErr: mockErr, expectedErr: "cannot connect to TPM: some error"},
		{tpmEnabled: false, expectedErr: "TPM device is not enabled"},
		{tpmEnabled: true, missingFile: true, buildProfileErr: `cannot build EFI image load sequences: file .*\/file.efi does not exist`},
		{tpmEnabled: true, addPCRProfileErr: mockErr, buildProfileErr: `cannot add EFI secure boot and boot manager policy profiles: some error`},
		{tpmEnabled: true, addSystemdEFIStubErr: mockErr, buildProfileErr: `cannot add systemd EFI stub profile: some error`},
		{tpmEnabled: true, addSnapModelErr: mockErr, buildProfileErr: `cannot add snap model profile: some error`},
		{tpmEnabled: true, readSealedKeyObjectErr: mockErr, expectedErr: "cannot read key file .*: some error"},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "cannot update legacy PCR protection policy: some error", oldKeyFiles: true},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "cannot update PCR protection policy: some error"},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "cannot update legacy PCR protection policy: some error", oldKeyFiles: true},
		{tpmEnabled: true, revokeErr: errors.New("revoke error"), resealCalls: 1, revokeCalls: 1, expectedErr: "cannot revoke old PCR protection policies: revoke error", oldKeyFiles: true},
	} {
		mockTPMPolicyAuthKey := []byte{1, 3, 3, 7}
		mockTPMPolicyAuthKeyFile := filepath.Join(c.MkDir(), "policy-auth-key-file")
		err := os.WriteFile(mockTPMPolicyAuthKeyFile, mockTPMPolicyAuthKey, 0600)
		c.Assert(err, IsNil)

		mockEFI := bootloader.NewBootFile("", filepath.Join(c.MkDir(), "file.efi"), bootloader.RoleRecovery)
		if !tc.missingFile {
			err := os.WriteFile(mockEFI.Path, nil, 0644)
			c.Assert(err, IsNil)
		}

		modelParams := []*secboot.SealKeyModelParams{
			{
				EFILoadChains:  []*secboot.LoadChain{secboot.NewLoadChain(mockEFI)},
				KernelCmdlines: []string{"cmdline"},
				Model:          &asserts.Model{},
			},
		}

		sequences := sb_efi.NewImageLoadSequences().Append(
			sb_efi.NewImageLoadActivity(
				sb_efi.NewFileImage(mockEFI.Path),
			),
		)

		addPCRProfileCalls := 0
		restore := secboot.MockSbEfiAddPCRProfile(func(pcrAlg tpm2.HashAlgorithmId, branch *sb_tpm2.PCRProtectionProfileBranch, loadSequences *sb_efi.ImageLoadSequences, options ...sb_efi.PCRProfileOption) error {
			addPCRProfileCalls++
			c.Assert(pcrAlg, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(loadSequences, DeepEquals, sequences)
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
			PCRProfile:           pcrProfile,
			KeyFiles:             []string{keyFile, keyFile2},
			TPMPolicyAuthKeyFile: mockTPMPolicyAuthKeyFile,
		}

		numMockSealedKeyObjects := len(myParams.KeyFiles)
		mockSealedKeyObjects := make([]*sb_tpm2.SealedKeyObject, 0, numMockSealedKeyObjects)
		mockKeyDatas := make([]*sb.KeyData, 0, numMockSealedKeyObjects)
		for range myParams.KeyFiles {
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
			c.Assert(keyfile, Equals, myParams.KeyFiles[readSealedKeyObjectCalls-1])
			if tc.oldKeyFiles {
				kl.LoadSealedKeyObject(mockSealedKeyObjects[readSealedKeyObjectCalls-1])
				return tc.readSealedKeyObjectErr
			} else {
				kl.LoadKeyData(mockKeyDatas[readSealedKeyObjectCalls-1])
				return tc.readSealedKeyObjectErr
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
			c.Assert(keyFile, testutil.FilePresent)
			c.Assert(keyFile2, testutil.FilePresent)
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
			KeyFile:               "keyfile",
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

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingKeyBadDisk(c *C) {
	disk := &disks.MockDiskMapping{}
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, ErrorMatches, `filesystem label "ubuntu-save-enc" not found`)
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{})
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingKeyUUIDError(c *C) {
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

	unlockRes, err := secboot.UnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, ErrorMatches, "mocked uuid error")
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		PartDevice:  "/dev/disk/by-uuid/321-321-321",
		IsEncrypted: true,
	})
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingKeyHappy(c *C) {
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
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, IsNil)
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		PartDevice:   "/dev/disk/by-uuid/321-321-321",
		FsDevice:     "/dev/mapper/ubuntu-save-random-uuid-123-123",
		IsEncrypted:  true,
		UnlockMethod: secboot.UnlockedWithKey,
	})
}

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingKeyErr(c *C) {
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
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", []byte("fooo"))
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

// this test that v1 hooks and raw binary v1 created sealedKey files still work
func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV1AndV1GeneratedSealedKeyFile(c *C) {
	// The v1 hooks will just return raw bytes. This is deprecated but
	// we need to keep compatbility with the v1 implementation because
	// there is a project "denver" that ships with v1 hooks.
	var reqs []*fde.RevealKeyRequest
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		reqs = append(reqs, req)
		return []byte("unsealed-key-64-chars-long-when-not-json-to-match-denver-project"), nil
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

	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte, options *sb.ActivateVolumeOptions) error {
		c.Errorf("unexpected error")
		return fmt.Errorf("unexpected error")
	})
	defer restore()

	mockSealedKeyObject, err := sb_tpm2.ReadSealedKeyObjectFromFile(filepath.Join("test-data", "keyfile"))
	c.Assert(err, IsNil)
	reader, err := sb.NewFileKeyDataReader(filepath.Join("test-data", "keydata"))
	c.Assert(err, IsNil)
	mockKeyData, err := sb.ReadKeyData(reader)
	c.Assert(err, IsNil)

	restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
		c.Check(hintExpectFDEHook, Equals, true)
		c.Check(keyfile, Equals, "the-key-file")
		kl.LoadKeyData(mockKeyData)
		kl.LoadSealedKeyObject(mockSealedKeyObject)
		return nil
	})
	defer restore()

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		activated++
		c.Assert(keys, HasLen, 1)
		c.Check(keys[0], DeepEquals, mockKeyData)
		return nil
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
	// FIXME: maybe we should remove this test
	c.Check(reqs, HasLen, 0)
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

func (s *secbootSuite) TestSealKeysWithFDESetupHookHappy(c *C) {
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

	tmpdir := c.MkDir()
	key1Fn := filepath.Join(tmpdir, "key1.key")
	key2Fn := filepath.Join(tmpdir, "key2.key")
	auxKeyFn := filepath.Join(tmpdir, "aux-key")
	params := secboot.SealKeysWithFDESetupHookParams{
		Model:      fakeModel,
		AuxKeyFile: auxKeyFn,
	}
	err := secboot.SealKeysWithFDESetupHook(runFDESetupHook,
		[]secboot.SealKeyRequest{
			{BootstrappedContainer: secboot.CreateMockBootstrappedContainer(), KeyName: "key1", KeyFile: key1Fn},
			{BootstrappedContainer: secboot.CreateMockBootstrappedContainer(), KeyName: "key2", KeyFile: key2Fn},
		}, &params)
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	c.Check(runFDESetupHookReqs, HasLen, 2)
	c.Check(runFDESetupHookReqs[0].Op, Equals, "initial-setup")
	c.Check(runFDESetupHookReqs[1].Op, Equals, "initial-setup")
	c.Check(runFDESetupHookReqs[0].KeyName, Equals, "key1")
	c.Check(runFDESetupHookReqs[1].KeyName, Equals, "key2")
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

	mockSealedKeyObject, err := sb_tpm2.ReadSealedKeyObjectFromFile(filepath.Join("test-data", "keyfile"))
	c.Assert(err, IsNil)
	reader, err := sb.NewFileKeyDataReader(filepath.Join("test-data", "keydata"))
	c.Assert(err, IsNil)
	mockKeyData, err := sb.ReadKeyData(reader)
	c.Assert(err, IsNil)

	restore = secboot.MockReadKeyFile(func(keyfile string, kl secboot.KeyLoader, hintExpectFDEHook bool) error {
		c.Check(hintExpectFDEHook, Equals, true)
		c.Check(keyfile, Equals, "the-key-file")
		kl.LoadKeyData(mockKeyData)
		kl.LoadSealedKeyObject(mockSealedKeyObject)
		return nil
	})
	defer restore()

	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte, options *sb.ActivateVolumeOptions) error {
		c.Errorf("unexpected calls")
		return fmt.Errorf("unexpected calls")
	})

	defer restore()

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, authRequestor sb.AuthRequestor, options *sb.ActivateVolumeOptions, keys ...*sb.KeyData) error {
		activated++
		c.Assert(keys, HasLen, 1)
		c.Check(keys[0], DeepEquals, mockKeyData)
		return nil
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
	// Maybe this test is superfluous
	c.Check(reqs, HasLen, 0)
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

func (s *secbootSuite) TestPCRHandleOfSealedKey(c *C) {
	d := c.MkDir()
	h, err := secboot.PCRHandleOfSealedKey(filepath.Join(d, "not-found"))
	c.Assert(err, ErrorMatches, "cannot read key file .*/not-found:.* no such file or directory")
	c.Assert(h, Equals, uint32(0))

	skf := filepath.Join(d, "sealed-key")
	// partially valid sealed key with correct header magic
	c.Assert(os.WriteFile(skf, []byte{0x55, 0x53, 0x4b, 0x24, 1, 1, 1, 'k', 'e', 'y', 1, 1, 1}, 0644), IsNil)
	h, err = secboot.PCRHandleOfSealedKey(skf)
	c.Assert(err, ErrorMatches, "(?s)cannot read key file .*: invalid key data: cannot unmarshal AFIS header: .*")
	c.Check(h, Equals, uint32(0))

	// TODO simulate the happy case, which needs a real (or at least
	// partially mocked) sealed key object, which could be obtained using
	// go-tpm2/testutil, but that has a dependency on an older version of
	// snapd API and cannot be imported or procure a valid sealed key binary
	// which unfortunately there are no examples of the secboot/tpm2 test
	// code
}

func (s *secbootSuite) TestReleasePCRResourceHandles(c *C) {
	_, restore := mockSbTPMConnection(c, fmt.Errorf("mock err"))
	defer restore()

	err := secboot.ReleasePCRResourceHandles(0x1234, 0x2345)
	c.Assert(err, ErrorMatches, "cannot connect to TPM device: mock err")

	conn, restore := mockSbTPMConnection(c, nil)
	defer restore()

	var handles []tpm2.Handle
	restore = secboot.MockTPMReleaseResources(func(tpm *sb_tpm2.Connection, handle tpm2.Handle) error {
		c.Check(tpm, Equals, conn)
		handles = append(handles, handle)
		switch handle {
		case tpm2.Handle(0xeeeeee):
			return fmt.Errorf("mock release error 1")
		case tpm2.Handle(0xeeeeef):
			return fmt.Errorf("mock release error 2")
		}
		return nil
	})
	defer restore()

	// many handles
	err = secboot.ReleasePCRResourceHandles(0x1234, 0x2345)
	c.Assert(err, IsNil)
	c.Check(handles, DeepEquals, []tpm2.Handle{
		tpm2.Handle(0x1234), tpm2.Handle(0x2345),
	})

	// single handle
	handles = nil
	err = secboot.ReleasePCRResourceHandles(0x1234)
	c.Assert(err, IsNil)
	c.Check(handles, DeepEquals, []tpm2.Handle{tpm2.Handle(0x1234)})

	// an error case
	handles = nil
	err = secboot.ReleasePCRResourceHandles(0x1234, 0xeeeeee, 0x2345, 0xeeeeef)
	c.Assert(err, ErrorMatches, `
cannot release TPM resources for 2 handles:
handle 0xeeeeee: mock release error 1
handle 0xeeeeef: mock release error 2`[1:])
	c.Check(handles, DeepEquals, []tpm2.Handle{
		tpm2.Handle(0x1234), tpm2.Handle(0xeeeeee), tpm2.Handle(0x2345), tpm2.Handle(0xeeeeef),
	})
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

func (s *secbootSuite) TestRenameOrDeleteKeys(c *C) {
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

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, IsNil)

	c.Check(expectedRenames, HasLen, 0)
}

func (s *secbootSuite) TestRenameOrDeleteKeysBadInput(c *C) {
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

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `internal error: keyslot name slot-b used as source and target of a rename`)
}

func (s *secbootSuite) TestRenameOrDeleteKeysNameExists(c *C) {
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

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `slot name slot-b is already in use`)
}

func (s *secbootSuite) TestRenameOrDeleteKeysNoRename(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	expectedRemovals := map[string]bool{
		"slot-b": true,
		"slot-c": true,
	}

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		return sb.ErrMissingCryptsetupFeature
	})()

	defer secboot.MockDeleteLUKS2ContainerKey(func(devicePath, slotName string) error {
		c.Check(devicePath, Equals, "/dev/foo")
		_, expected := expectedRemovals[slotName]
		c.Check(expected, Equals, true)
		delete(expectedRemovals, slotName)
		return nil
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, IsNil)

	c.Check(expectedRemovals, HasLen, 0)
}

func (s *secbootSuite) TestRenameOrDeleteKeysListError(c *C) {
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

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `cannot list slots in partition save partition: some error`)
}

func (s *secbootSuite) TestRenameOrDeleteKeysRenameError(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		return fmt.Errorf("some other error")
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

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `cannot rename container key: some other error`)
}

func (s *secbootSuite) TestRenameOrDeleteKeysDeleteError(c *C) {
	defer secboot.MockListLUKS2ContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		c.Check(devicePath, Equals, "/dev/foo")
		return []string{"slot-a", "slot-b", "slot-c"}, nil
	})()

	defer secboot.MockRenameLUKS2ContainerKey(func(devicePath, slotName, renameTo string) error {
		return sb.ErrMissingCryptsetupFeature
	})()

	defer secboot.MockDeleteLUKS2ContainerKey(func(devicePath, slotName string) error {
		return fmt.Errorf("some error")
	})()

	toRename := map[string]string{
		"slot-b": "new-slot-b",
		"slot-c": "new-slot-c",
		"slot-d": "new-slot-d",
	}

	err := secboot.RenameOrDeleteKeys("/dev/foo", toRename)
	c.Assert(err, ErrorMatches, `cannot remove old container key: some error`)
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
