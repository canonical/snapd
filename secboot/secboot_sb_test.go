// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

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
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/linux"
	sb "github.com/snapcore/secboot"
	sb_efi "github.com/snapcore/secboot/efi"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

type secbootSuite struct {
	testutil.BaseTest
}

var _ = Suite(&secbootSuite{})

func (s *secbootSuite) SetUpTest(c *C) {
	rootDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(rootDir, "/run"), 0755)
	c.Assert(err, IsNil)
	dirs.SetRootDir(rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *secbootSuite) TestCheckTPMKeySealingSupported(c *C) {
	c.Check(secboot.WithSecbootSupport, Equals, true)

	sbEmpty := []uint8{}
	sbEnabled := []uint8{1}
	sbDisabled := []uint8{0}
	efiNotSupported := []uint8(nil)
	tpmErr := errors.New("TPM error")

	type testCase struct {
		tpmErr     error
		tpmEnabled bool
		sbData     []uint8
		err        string
	}
	for i, tc := range []testCase{
		// happy case
		{tpmErr: nil, tpmEnabled: true, sbData: sbEnabled, err: ""},
		// secure boot EFI var is empty
		{tpmErr: nil, tpmEnabled: true, sbData: sbEmpty, err: "secure boot variable does not exist"},
		// secure boot is disabled
		{tpmErr: nil, tpmEnabled: true, sbData: sbDisabled, err: "secure boot is disabled"},
		// EFI not supported
		{tpmErr: nil, tpmEnabled: true, sbData: efiNotSupported, err: "not a supported EFI system"},
		// TPM connection error
		{tpmErr: tpmErr, sbData: sbEnabled, err: "cannot connect to TPM device: TPM error"},
		// TPM was detected but it's not enabled
		{tpmErr: nil, tpmEnabled: false, sbData: sbEnabled, err: "TPM device is not enabled"},
		// No TPM device
		{tpmErr: sb_tpm2.ErrNoTPM2Device, sbData: sbEnabled, err: "cannot connect to TPM device: no TPM2 device is available"},
	} {
		c.Logf("%d: %v %v %v %q", i, tc.tpmErr, tc.tpmEnabled, tc.sbData, tc.err)

		_, restore := mockSbTPMConnection(c, tc.tpmErr)
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

		err := secboot.CheckTPMKeySealingSupported()
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
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

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncrypted(c *C) {

	// setup mock disks to use for locating the partition
	// restore := disks.MockMountPointDisksToPartitionMapping()
	// defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	mockDiskWithoutAnyDev := &disks.MockDiskMapping{}

	mockDiskWithUnencDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "name",
				PartitionUUID:   "unenc-dev-partuuid",
			},
		},
	}

	for idx, tc := range []struct {
		tpmErr              error
		tpmEnabled          bool  // TPM storage and endorsement hierarchies disabled, only relevant if TPM available
		hasEncdev           bool  // an encrypted device exists
		rkAllow             bool  // allow recovery key activation
		rkErr               error // recovery key unlock error, only relevant if TPM not available
		activated           bool  // the activation operation succeeded
		activateErr         error // the activation error
		err                 string
		skipDiskEnsureCheck bool // whether to check to ensure the mock disk contains the device label
		expUnlockMethod     secboot.UnlockMethod
		disk                *disks.MockDiskMapping
	}{
		{
			// happy case with tpm and encrypted device
			tpmEnabled: true, hasEncdev: true,
			activated:       true,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithSealedKey,
		}, {
			// device activation fails
			tpmEnabled: true, hasEncdev: true,
			err:  "cannot activate encrypted device .*: activation error",
			disk: mockDiskWithEncDev,
		}, {
			// device activation fails
			tpmEnabled: true, hasEncdev: true,
			err:  "cannot activate encrypted device .*: activation error",
			disk: mockDiskWithEncDev,
		}, {
			// happy case without encrypted device
			tpmEnabled: true,
			disk:       mockDiskWithUnencDev,
		}, {
			// happy case with tpm and encrypted device, activation
			// with recovery key
			tpmEnabled: true, hasEncdev: true, activated: true,
			activateErr:     sb.ErrRecoveryKeyUsed,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithRecoveryKey,
		}, {
			// tpm error, no encrypted device
			tpmErr: errors.New("tpm error"),
			disk:   mockDiskWithUnencDev,
		}, {
			// tpm error, has encrypted device
			tpmErr: errors.New("tpm error"), hasEncdev: true,
			err:  `cannot unlock encrypted device "name": tpm error`,
			disk: mockDiskWithEncDev,
		}, {
			// tpm disabled, no encrypted device
			disk: mockDiskWithUnencDev,
		}, {
			// tpm disabled, has encrypted device, unlocked using the recovery key
			hasEncdev:       true,
			rkAllow:         true,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithRecoveryKey,
		}, {
			// tpm disabled, has encrypted device, recovery key unlocking fails
			hasEncdev: true, rkErr: errors.New("cannot unlock with recovery key"),
			rkAllow: true,
			disk:    mockDiskWithEncDev,
			err:     `cannot unlock encrypted device ".*/enc-dev-partuuid": cannot unlock with recovery key`,
		}, {
			// no tpm, has encrypted device, unlocked using the recovery key
			tpmErr: sb_tpm2.ErrNoTPM2Device, hasEncdev: true,
			rkAllow:         true,
			disk:            mockDiskWithEncDev,
			expUnlockMethod: secboot.UnlockedWithRecoveryKey,
		}, {
			// no tpm, has encrypted device, unlocking with recovery key not allowed
			tpmErr: sb_tpm2.ErrNoTPM2Device, hasEncdev: true,
			disk: mockDiskWithEncDev,
			err:  `cannot activate encrypted device ".*/enc-dev-partuuid": activation error`,
		}, {
			// no tpm, has encrypted device, recovery key unlocking fails
			rkErr:  errors.New("cannot unlock with recovery key"),
			tpmErr: sb_tpm2.ErrNoTPM2Device, hasEncdev: true,
			rkAllow: true,
			disk:    mockDiskWithEncDev,
			err:     `cannot unlock encrypted device ".*/enc-dev-partuuid": cannot unlock with recovery key`,
		}, {
			// no tpm, no encrypted device
			tpmErr: sb_tpm2.ErrNoTPM2Device,
			disk:   mockDiskWithUnencDev,
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
		randomUUID := fmt.Sprintf("random-uuid-for-test-%d", idx)
		restore := secboot.MockRandomKernelUUID(func() string {
			return randomUUID
		})
		defer restore()

		c.Logf("tc %v: %+v", idx, tc)
		_, restoreConnect := mockSbTPMConnection(c, tc.tpmErr)
		defer restoreConnect()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		defaultDevice := "name"

		fsLabel := defaultDevice
		if tc.hasEncdev {
			fsLabel += "-enc"
		}

		partuuid := ""
		if !tc.skipDiskEnsureCheck {
			for _, p := range tc.disk.Structure {
				if p.FilesystemLabel == fsLabel {
					partuuid = p.PartitionUUID
					break
				}
			}
			c.Assert(partuuid, Not(Equals), "", Commentf("didn't find fs label %s in disk", fsLabel))
		}

		devicePath := filepath.Join("/dev/disk/by-partuuid", partuuid)

		keyPath := filepath.Join("test-data", "keyfile")
		kd, err := sb_tpm2.NewKeyDataFromSealedKeyObjectFile(keyPath)
		c.Assert(err, IsNil)
		expectedID, err := kd.UniqueID()
		c.Assert(err, IsNil)

		restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
			c.Assert(volumeName, Equals, "name-"+randomUUID)
			c.Assert(sourceDevicePath, Equals, devicePath)
			c.Assert(keyData, NotNil)
			uID, err := keyData.UniqueID()
			c.Assert(err, IsNil)
			c.Check(uID, DeepEquals, expectedID)
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
			if !tc.activated && tc.activateErr == nil {
				return nil, errors.New("activation error")
			}
			return nil, tc.activateErr
		})
		defer restore()

		restore = secboot.MockSbActivateVolumeWithRecoveryKey(func(name, device string, keyReader io.Reader,
			options *sb.ActivateVolumeOptions) error {
			if !tc.rkAllow {
				c.Fatalf("unexpected attempt to activate with recovery key")
				return fmt.Errorf("unexpected call")
			}
			return tc.rkErr
		})
		defer restore()

		opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
			AllowRecoveryKey: tc.rkAllow,
		}
		unlockRes, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(tc.disk, defaultDevice, keyPath, opts)
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
	}
}

func (s *secbootSuite) TestEFIImageFromBootFile(c *C) {
	tmpDir := c.MkDir()

	// set up some test files
	existingFile := filepath.Join(tmpDir, "foo")
	err := ioutil.WriteFile(existingFile, nil, 0644)
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
			efiImage: sb_efi.FileImage(existingFile),
		},
		{
			// missing EFI image
			bootFile: bootloader.NewBootFile("", missingFile, bootloader.RoleRecovery),
			err:      fmt.Sprintf("file %s/bar does not exist", tmpDir),
		},
		{
			// happy case for snap file
			bootFile: bootloader.NewBootFile(snapFile, "rel", bootloader.RoleRecovery),
			efiImage: sb_efi.SnapFileImage{Container: snapf, FileName: "rel"},
		},
		{
			// invalid snap file
			bootFile: bootloader.NewBootFile(existingFile, "rel", bootloader.RoleRecovery),
			err:      fmt.Sprintf(`"%s/foo" is not a snap or snapdir`, tmpDir),
		},
		{
			// missing snap file
			bootFile: bootloader.NewBootFile(missingFile, "rel", bootloader.RoleRecovery),
			err:      fmt.Sprintf(`"%s/bar" is not a snap or snapdir`, tmpDir),
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

func (s *secbootSuite) TestSealKey(c *C) {
	mockErr := errors.New("some error")

	for _, tc := range []struct {
		tpmErr               error
		tpmEnabled           bool
		missingFile          bool
		badSnapFile          bool
		skipProvision        bool
		addEFISbPolicyErr    error
		addEFIBootManagerErr error
		addSystemdEFIStubErr error
		addSnapModelErr      error
		provisioningErr      error
		sealErr              error
		provisioningCalls    int
		sealCalls            int
		expectedErr          string
	}{
		{tpmErr: mockErr, expectedErr: "cannot connect to TPM: some error"},
		{tpmEnabled: false, expectedErr: "TPM device is not enabled"},
		{tpmEnabled: true, missingFile: true, expectedErr: "cannot build EFI image load sequences: file /does/not/exist does not exist"},
		{tpmEnabled: true, badSnapFile: true, expectedErr: `.*/kernel.snap" is not a snap or snapdir`},
		{tpmEnabled: true, addEFISbPolicyErr: mockErr, expectedErr: "cannot add EFI secure boot policy profile: some error"},
		{tpmEnabled: true, addEFIBootManagerErr: mockErr, expectedErr: "cannot add EFI boot manager profile: some error"},
		{tpmEnabled: true, addSystemdEFIStubErr: mockErr, expectedErr: "cannot add systemd EFI stub profile: some error"},
		{tpmEnabled: true, addSnapModelErr: mockErr, expectedErr: "cannot add snap model profile: some error"},
		{tpmEnabled: true, provisioningErr: mockErr, provisioningCalls: 1, expectedErr: "cannot provision TPM: some error"},
		{tpmEnabled: true, sealErr: mockErr, provisioningCalls: 1, sealCalls: 1, expectedErr: "some error"},
		{tpmEnabled: true, skipProvision: true, provisioningCalls: 0, sealCalls: 1, expectedErr: ""},
		{tpmEnabled: true, provisioningCalls: 1, sealCalls: 1, expectedErr: ""},
	} {
		tmpDir := c.MkDir()
		var mockBF []bootloader.BootFile
		for _, name := range []string{"a", "b", "c", "d"} {
			mockFileName := filepath.Join(tmpDir, name)
			err := ioutil.WriteFile(mockFileName, nil, 0644)
			c.Assert(err, IsNil)
			mockBF = append(mockBF, bootloader.NewBootFile("", mockFileName, bootloader.RoleRecovery))
		}

		if tc.missingFile {
			mockBF[0].Path = "/does/not/exist"
		}

		var kernelSnap snap.Container
		snapPath := filepath.Join(tmpDir, "kernel.snap")
		if tc.badSnapFile {
			err := ioutil.WriteFile(snapPath, nil, 0644)
			c.Assert(err, IsNil)
		} else {
			var err error
			kernelSnap, err = createMockSnapFile(c.MkDir(), snapPath, "kernel")
			c.Assert(err, IsNil)
		}

		mockBF = append(mockBF, bootloader.NewBootFile(snapPath, "kernel.efi", bootloader.RoleRecovery))

		myAuthKey := &ecdsa.PrivateKey{}

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
			TPMPolicyAuthKey:       myAuthKey,
			TPMPolicyAuthKeyFile:   filepath.Join(tmpDir, "policy-auth-key-file"),
			TPMLockoutAuthFile:     filepath.Join(tmpDir, "lockout-auth-file"),
			TPMProvision:           !tc.skipProvision,
			PCRPolicyCounterHandle: 42,
		}

		myKey := secboot.EncryptionKey{}
		myKey2 := secboot.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
			myKey2[i] = byte(128 + i)
		}

		myKeys := []secboot.SealKeyRequest{
			{
				Key:     myKey,
				KeyFile: "keyfile",
			},
			{
				Key:     myKey2,
				KeyFile: "keyfile2",
			},
		}

		// events for
		// a -> kernel
		sequences1 := []*sb_efi.ImageLoadEvent{
			{
				Source: sb_efi.Firmware,
				Image:  sb_efi.FileImage(mockBF[0].Path),
				Next: []*sb_efi.ImageLoadEvent{
					{
						Source: sb_efi.Shim,
						Image: sb_efi.SnapFileImage{
							Container: kernelSnap,
							FileName:  "kernel.efi",
						},
					},
				},
			},
		}

		// "cdk" events for
		// c -> kernel OR
		// d -> kernel
		cdk := []*sb_efi.ImageLoadEvent{
			{
				Source: sb_efi.Shim,
				Image:  sb_efi.FileImage(mockBF[2].Path),
				Next: []*sb_efi.ImageLoadEvent{
					{
						Source: sb_efi.Shim,
						Image: sb_efi.SnapFileImage{
							Container: kernelSnap,
							FileName:  "kernel.efi",
						},
					},
				},
			},
			{
				Source: sb_efi.Shim,
				Image:  sb_efi.FileImage(mockBF[3].Path),
				Next: []*sb_efi.ImageLoadEvent{
					{
						Source: sb_efi.Shim,
						Image: sb_efi.SnapFileImage{
							Container: kernelSnap,
							FileName:  "kernel.efi",
						},
					},
				},
			},
		}

		// events for
		// a -> "cdk"
		// b -> "cdk"
		sequences2 := []*sb_efi.ImageLoadEvent{
			{
				Source: sb_efi.Firmware,
				Image:  sb_efi.FileImage(mockBF[0].Path),
				Next:   cdk,
			},
			{
				Source: sb_efi.Firmware,
				Image:  sb_efi.FileImage(mockBF[1].Path),
				Next:   cdk,
			},
		}

		tpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		// mock adding EFI secure boot policy profile
		var pcrProfile *sb_tpm2.PCRProtectionProfile
		addEFISbPolicyCalls := 0
		restore = secboot.MockSbEfiAddSecureBootPolicyProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.SecureBootPolicyProfileParams) error {
			addEFISbPolicyCalls++
			pcrProfile = profile
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			switch addEFISbPolicyCalls {
			case 1:
				c.Assert(params.LoadSequences, DeepEquals, sequences1)
			case 2:
				c.Assert(params.LoadSequences, DeepEquals, sequences2)
			default:
				c.Error("AddSecureBootPolicyProfile shouldn't be called a third time")
			}
			return tc.addEFISbPolicyErr
		})
		defer restore()

		// mock adding EFI boot manager profile
		addEFIBootManagerCalls := 0
		restore = secboot.MockSbEfiAddBootManagerProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.BootManagerProfileParams) error {
			addEFIBootManagerCalls++
			c.Assert(profile, Equals, pcrProfile)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			switch addEFISbPolicyCalls {
			case 1:
				c.Assert(params.LoadSequences, DeepEquals, sequences1)
			case 2:
				c.Assert(params.LoadSequences, DeepEquals, sequences2)
			default:
				c.Error("AddBootManagerProfile shouldn't be called a third time")
			}
			return tc.addEFIBootManagerErr
		})
		defer restore()

		// mock adding systemd EFI stub profile
		addSystemdEfiStubCalls := 0
		restore = secboot.MockSbEfiAddSystemdStubProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.SystemdStubProfileParams) error {
			addSystemdEfiStubCalls++
			c.Assert(profile, Equals, pcrProfile)
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
		restore = secboot.MockSbAddSnapModelProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_tpm2.SnapModelProfileParams) error {
			addSnapModelCalls++
			c.Assert(profile, Equals, pcrProfile)
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

		// mock provisioning
		provisioningCalls := 0
		restore = secboot.MockProvisionTPM(func(t *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, newLockoutAuth []byte) error {
			provisioningCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(mode, Equals, sb_tpm2.ProvisionModeFull)
			c.Assert(myParams.TPMLockoutAuthFile, testutil.FilePresent)
			return tc.provisioningErr
		})
		defer restore()

		// mock sealing
		sealCalls := 0
		restore = secboot.MockSbSealKeyToTPMMultiple(func(t *sb_tpm2.Connection, kr []*sb_tpm2.SealKeyRequest, params *sb_tpm2.KeyCreationParams) (sb_tpm2.PolicyAuthKey, error) {
			sealCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(kr, DeepEquals, []*sb_tpm2.SealKeyRequest{{Key: myKey, Path: "keyfile"}, {Key: myKey2, Path: "keyfile2"}})
			c.Assert(params.AuthKey, Equals, myAuthKey)
			c.Assert(params.PCRPolicyCounterHandle, Equals, tpm2.Handle(42))
			return sb_tpm2.PolicyAuthKey{}, tc.sealErr
		})
		defer restore()

		// mock TPM enabled check
		restore = secboot.MockIsTPMEnabled(func(t *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		err := secboot.SealKeys(myKeys, &myParams)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
			c.Assert(addEFISbPolicyCalls, Equals, 2)
			c.Assert(addSystemdEfiStubCalls, Equals, 2)
			c.Assert(addSnapModelCalls, Equals, 2)
			c.Assert(osutil.FileExists(myParams.TPMPolicyAuthKeyFile), Equals, true)
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
		}
		c.Assert(provisioningCalls, Equals, tc.provisioningCalls)
		c.Assert(sealCalls, Equals, tc.sealCalls)

	}
}

func (s *secbootSuite) TestResealKey(c *C) {
	mockErr := errors.New("some error")

	for _, tc := range []struct {
		tpmErr                 error
		tpmEnabled             bool
		missingFile            bool
		addEFISbPolicyErr      error
		addEFIBootManagerErr   error
		addSystemdEFIStubErr   error
		addSnapModelErr        error
		readSealedKeyObjectErr error
		provisioningErr        error
		resealErr              error
		resealCalls            int
		revokeErr              error
		revokeCalls            int
		expectedErr            string
	}{
		// happy case
		{tpmEnabled: true, resealCalls: 1, revokeCalls: 1, expectedErr: ""},

		// unhappy cases
		{tpmErr: mockErr, expectedErr: "cannot connect to TPM: some error"},
		{tpmEnabled: false, expectedErr: "TPM device is not enabled"},
		{tpmEnabled: true, missingFile: true, expectedErr: "cannot build EFI image load sequences: file .*/file.efi does not exist"},
		{tpmEnabled: true, addEFISbPolicyErr: mockErr, expectedErr: "cannot add EFI secure boot policy profile: some error"},
		{tpmEnabled: true, addEFIBootManagerErr: mockErr, expectedErr: "cannot add EFI boot manager profile: some error"},
		{tpmEnabled: true, addSystemdEFIStubErr: mockErr, expectedErr: "cannot add systemd EFI stub profile: some error"},
		{tpmEnabled: true, addSnapModelErr: mockErr, expectedErr: "cannot add snap model profile: some error"},
		{tpmEnabled: true, readSealedKeyObjectErr: mockErr, expectedErr: "some error"},
		{tpmEnabled: true, resealErr: mockErr, resealCalls: 1, expectedErr: "some error"},
		{tpmEnabled: true, revokeErr: errors.New("revoke error"), resealCalls: 1, revokeCalls: 1, expectedErr: "revoke error"},
	} {
		mockTPMPolicyAuthKey := []byte{1, 3, 3, 7}
		mockTPMPolicyAuthKeyFile := filepath.Join(c.MkDir(), "policy-auth-key-file")
		err := ioutil.WriteFile(mockTPMPolicyAuthKeyFile, mockTPMPolicyAuthKey, 0600)
		c.Assert(err, IsNil)

		mockEFI := bootloader.NewBootFile("", filepath.Join(c.MkDir(), "file.efi"), bootloader.RoleRecovery)
		if !tc.missingFile {
			err := ioutil.WriteFile(mockEFI.Path, nil, 0644)
			c.Assert(err, IsNil)
		}

		tmpdir := c.MkDir()
		keyFile := filepath.Join(tmpdir, "keyfile")
		keyFile2 := filepath.Join(tmpdir, "keyfile2")
		myParams := &secboot.ResealKeysParams{
			ModelParams: []*secboot.SealKeyModelParams{
				{
					EFILoadChains:  []*secboot.LoadChain{secboot.NewLoadChain(mockEFI)},
					KernelCmdlines: []string{"cmdline"},
					Model:          &asserts.Model{},
				},
			},
			KeyFiles:             []string{keyFile, keyFile2},
			TPMPolicyAuthKeyFile: mockTPMPolicyAuthKeyFile,
		}

		numMockSealedKeyObjects := len(myParams.KeyFiles)
		mockSealedKeyObjects := make([]*sb_tpm2.SealedKeyObject, 0, numMockSealedKeyObjects)
		for range myParams.KeyFiles {
			// Copy of
			// https://github.com/snapcore/secboot/blob/master/internal/compattest/testdata/v1/key
			// To create full looking
			// mockSealedKeyObjects, although {},{} would
			// have been enough as well
			mockSealedKeyFile := filepath.Join("test-data", "keyfile")
			mockSealedKeyObject, err := sb_tpm2.ReadSealedKeyObjectFromFile(mockSealedKeyFile)
			c.Assert(err, IsNil)
			mockSealedKeyObjects = append(mockSealedKeyObjects, mockSealedKeyObject)
		}

		sequences := []*sb_efi.ImageLoadEvent{
			{
				Source: sb_efi.Firmware,
				Image:  sb_efi.FileImage(mockEFI.Path),
			},
		}

		// mock TPM connection
		tpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		// mock TPM enabled check
		restore = secboot.MockIsTPMEnabled(func(t *sb_tpm2.Connection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		// mock adding EFI secure boot policy profile
		var pcrProfile *sb_tpm2.PCRProtectionProfile
		addEFISbPolicyCalls := 0
		restore = secboot.MockSbEfiAddSecureBootPolicyProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.SecureBootPolicyProfileParams) error {
			addEFISbPolicyCalls++
			pcrProfile = profile
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.LoadSequences, DeepEquals, sequences)
			return tc.addEFISbPolicyErr
		})
		defer restore()

		// mock adding EFI boot manager profile
		addEFIBootManagerCalls := 0
		restore = secboot.MockSbEfiAddBootManagerProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.BootManagerProfileParams) error {
			addEFIBootManagerCalls++
			c.Assert(profile, Equals, pcrProfile)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.LoadSequences, DeepEquals, sequences)
			return tc.addEFIBootManagerErr
		})
		defer restore()

		// mock adding systemd EFI stub profile
		addSystemdEfiStubCalls := 0
		restore = secboot.MockSbEfiAddSystemdStubProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.SystemdStubProfileParams) error {
			addSystemdEfiStubCalls++
			c.Assert(profile, Equals, pcrProfile)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.PCRIndex, Equals, 12)
			c.Assert(params.KernelCmdlines, DeepEquals, myParams.ModelParams[0].KernelCmdlines)
			return tc.addSystemdEFIStubErr
		})
		defer restore()

		// mock adding snap model profile
		addSnapModelCalls := 0
		restore = secboot.MockSbAddSnapModelProfile(func(profile *sb_tpm2.PCRProtectionProfile, params *sb_tpm2.SnapModelProfileParams) error {
			addSnapModelCalls++
			c.Assert(profile, Equals, pcrProfile)
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			c.Assert(params.PCRIndex, Equals, 12)
			c.Assert(params.Models[0], DeepEquals, myParams.ModelParams[0].Model)
			return tc.addSnapModelErr
		})
		defer restore()

		// mock ReadSealedKeyObject
		readSealedKeyObjectCalls := 0
		restore = secboot.MockSbReadSealedKeyObjectFromFile(func(keyfile string) (*sb_tpm2.SealedKeyObject, error) {
			readSealedKeyObjectCalls++
			c.Assert(keyfile, Equals, myParams.KeyFiles[readSealedKeyObjectCalls-1])
			return mockSealedKeyObjects[readSealedKeyObjectCalls-1], tc.readSealedKeyObjectErr
		})
		defer restore()

		// mock PCR protection policy update
		resealCalls := 0
		restore = secboot.MockSbUpdateKeyPCRProtectionPolicyMultiple(func(t *sb_tpm2.Connection, keys []*sb_tpm2.SealedKeyObject, authKey sb_tpm2.PolicyAuthKey, profile *sb_tpm2.PCRProtectionProfile) error {
			resealCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(keys, DeepEquals, mockSealedKeyObjects)
			c.Assert(authKey, DeepEquals, sb_tpm2.PolicyAuthKey(mockTPMPolicyAuthKey))
			c.Assert(profile, Equals, pcrProfile)
			return tc.resealErr
		})
		defer restore()
		// mock PCR protection policy revoke
		revokeCalls := 0
		restore = secboot.MockSbSealedKeyObjectRevokeOldPCRProtectionPolicies(func(sko *sb_tpm2.SealedKeyObject, t *sb_tpm2.Connection, authKey sb_tpm2.PolicyAuthKey) error {
			revokeCalls++
			c.Assert(sko, Equals, mockSealedKeyObjects[0])
			c.Assert(t, Equals, tpm)
			c.Assert(authKey, DeepEquals, sb_tpm2.PolicyAuthKey(mockTPMPolicyAuthKey))
			return tc.revokeErr
		})
		defer restore()

		err = secboot.ResealKeys(myParams)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
			c.Assert(addEFISbPolicyCalls, Equals, 1)
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
			Key:     secboot.EncryptionKey{},
			KeyFile: "keyfile",
		},
	}
	myParams := secboot.SealKeysParams{
		TPMPolicyAuthKeyFile: "policy-auth-key-file",
		TPMLockoutAuthFile:   "lockout-auth-file",
	}

	err := secboot.SealKeys(myKeys, &myParams)
	c.Assert(err, ErrorMatches, "at least one set of model-specific parameters is required")
}

func createMockSnapFile(snapDir, snapPath, snapType string) (snap.Container, error) {
	snapYamlPath := filepath.Join(snapDir, "meta/snap.yaml")
	if err := os.MkdirAll(filepath.Dir(snapYamlPath), 0755); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(snapYamlPath, []byte("name: foo"), 0644); err != nil {
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

func (s *secbootSuite) TestUnlockEncryptedVolumeUsingKeyHappy(c *C) {
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-save-enc",
				PartitionUUID:   "123-123-123",
			},
		},
	}
	restore := secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-123-123"
	})
	defer restore()
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte,
		options *sb.ActivateVolumeOptions) error {
		c.Check(options, DeepEquals, &sb.ActivateVolumeOptions{})
		c.Check(key, DeepEquals, []byte("fooo"))
		c.Check(volumeName, Matches, "ubuntu-save-random-uuid-123-123")
		c.Check(sourceDevicePath, Equals, "/dev/disk/by-partuuid/123-123-123")
		return nil
	})
	defer restore()
	unlockRes, err := secboot.UnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", []byte("fooo"))
	c.Assert(err, IsNil)
	c.Check(unlockRes, DeepEquals, secboot.UnlockResult{
		PartDevice:   "/dev/disk/by-partuuid/123-123-123",
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
				PartitionUUID:   "123-123-123",
			},
		},
	}
	restore := secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-123-123"
	})
	defer restore()
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
		PartDevice:  "/dev/disk/by-partuuid/123-123-123",
		FsDevice:    "",
	})
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyErr(c *C) {
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		return nil, fmt.Errorf("helper error")
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
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}
	defaultDevice := "name"
	mockSealedKeyFile := makeMockSealedKeyFile(c, nil)

	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		_, _, err := keyData.RecoverKeys()
		if err != nil {
			return nil, err
		}
		c.Fatal("should not get this far")
		return nil, nil
	})
	defer restore()

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{}
	_, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)
	c.Assert(err, ErrorMatches, `cannot unlock encrypted partition: cannot recover keys because of an unexpected error: cannot run fde-reveal-key "reveal": helper error`)
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

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte, options *sb.ActivateVolumeOptions) error {
		activated++
		c.Check(string(key), Equals, "unsealed-key-64-chars-long-when-not-json-to-match-denver-project")
		return nil
	})
	defer restore()

	defaultDevice := "device-name"
	// note that we write a v1 created keyfile here, i.e. it's a raw
	// disk-key without any json
	mockSealedKeyFile := filepath.Join(c.MkDir(), "keyfile")
	sealedKeyContent := []byte("USK$sealed-key-not-json-to-match-denver-project")
	err := ioutil.WriteFile(mockSealedKeyFile, sealedKeyContent, 0600)
	c.Assert(err, IsNil)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{}
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
	c.Check(reqs[0].SealedKey, DeepEquals, sealedKeyContent)
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
	tmpdir := c.MkDir()

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

	key1 := secboot.EncryptionKey{1, 2, 3, 4}
	key2 := secboot.EncryptionKey{5, 6, 7, 8}
	auxKey := secboot.AuxKey{9, 10, 11, 12}
	key1Fn := filepath.Join(tmpdir, "key1.key")
	key2Fn := filepath.Join(tmpdir, "key2.key")
	auxKeyFn := filepath.Join(tmpdir, "aux-key")
	params := secboot.SealKeysWithFDESetupHookParams{
		Model:      fakeModel,
		AuxKey:     auxKey,
		AuxKeyFile: auxKeyFn,
	}
	err := secboot.SealKeysWithFDESetupHook(runFDESetupHook,
		[]secboot.SealKeyRequest{
			{Key: key1, KeyName: "key1", KeyFile: key1Fn},
			{Key: key2, KeyName: "key2", KeyFile: key2Fn},
		}, &params)
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	key1Payload := sb.MarshalKeys([]byte(key1), auxKey[:])
	key2Payload := sb.MarshalKeys([]byte(key2), auxKey[:])
	c.Check(runFDESetupHookReqs, DeepEquals, []*fde.SetupRequest{
		{Op: "initial-setup", Key: key1Payload, KeyName: "key1"},
		{Op: "initial-setup", Key: key2Payload, KeyName: "key2"},
	})
	// check that the sealed keys got written to the expected places
	for _, p := range []string{key1Fn, key2Fn} {
		c.Check(p, testutil.FilePresent)
	}
	c.Check(auxKeyFn, testutil.FileEquals, auxKey[:])

	// roundtrip to check what was written
	s.checkV2Key(c, key1Fn, sealedPrefix, key1, auxKey[:], fakeModel, &rawHandle1)
	nullHandle := json.RawMessage("null")
	s.checkV2Key(c, key2Fn, sealedPrefix, key2, auxKey[:], fakeModel, &nullHandle)
}

func (s *secbootSuite) TestSealKeysWithFDESetupHookSad(c *C) {
	tmpdir := c.MkDir()

	runFDESetupHook := func(req *fde.SetupRequest) ([]byte, error) {
		return nil, fmt.Errorf("hook failed")
	}

	key := secboot.EncryptionKey{1, 2, 3, 4}
	auxKey := secboot.AuxKey{5, 6, 7, 8}
	keyFn := filepath.Join(tmpdir, "key.key")
	auxKeyFn := filepath.Join(tmpdir, "aux-key")
	params := secboot.SealKeysWithFDESetupHookParams{
		Model:      fakeModel,
		AuxKey:     auxKey,
		AuxKeyFile: auxKeyFn,
	}
	err := secboot.SealKeysWithFDESetupHook(runFDESetupHook,
		[]secboot.SealKeyRequest{
			{Key: key, KeyName: "key1", KeyFile: keyFn},
		}, &params)
	c.Assert(err, ErrorMatches, "hook failed")
	c.Check(keyFn, testutil.FileAbsent)
	c.Check(auxKeyFn, testutil.FileAbsent)
}

func makeMockDiskKey() secboot.EncryptionKey {
	return secboot.EncryptionKey{0, 1, 2, 3, 4, 5}
}

func makeMockAuxKey() secboot.AuxKey {
	return secboot.AuxKey{6, 7, 8, 9}
}

func makeMockUnencryptedPayload() []byte {
	diskKey := makeMockDiskKey()
	auxKey := makeMockAuxKey()
	return sb.MarshalKeys([]byte(diskKey), auxKey[:])
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
	err := ioutil.WriteFile(mockSealedKeyFile, []byte(sealedKeyContent), 0600)
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

type mockSnapModelChecker struct {
	mockIsAuthorized bool
	mockError        error
}

func (c *mockSnapModelChecker) IsModelAuthorized(model sb.SnapModel) (bool, error) {
	if model.BrandID() != "my-brand" || model.Model() != "my-model" {
		return false, fmt.Errorf("not the test model")
	}
	return c.mockIsAuthorized, c.mockError
}
func (c *mockSnapModelChecker) VolumeName() string {
	return "volume-name"
}

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

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	expectedKey := makeMockDiskKey()
	expectedAuxKey := makeMockAuxKey()
	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		activated++
		c.Check(options.RecoveryKeyTries, Equals, 0)
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		key, auxKey, err := keyData.RecoverKeys()
		c.Assert(err, IsNil)
		c.Check([]byte(key), DeepEquals, []byte(expectedKey))
		c.Check([]byte(auxKey), DeepEquals, expectedAuxKey[:])
		modChecker := &mockSnapModelChecker{mockIsAuthorized: true}
		return modChecker, nil
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

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV2ModelUnauthorized(c *C) {
	restore := secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		activated++
		modChecker := &mockSnapModelChecker{mockIsAuthorized: false}
		return modChecker, nil
	})
	defer restore()

	deactivated := 0
	restore = secboot.MockSbDeactivateVolume(func(volumeName string) error {
		deactivated++
		c.Check(volumeName, Equals, "device-name-random-uuid-for-test")
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
	c.Assert(err, ErrorMatches, `cannot unlock volume: model my-brand/my-model not authorized`)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		IsEncrypted: true,
		PartDevice:  "/dev/disk/by-partuuid/enc-dev-partuuid",
	})
	c.Check(activated, Equals, 1)
	c.Check(deactivated, Equals, 1)
}

func (s *secbootSuite) TestUnlockVolumeUsingSealedKeyIfEncryptedFdeRevealKeyV2ModelCheckerError(c *C) {
	restore := secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		activated++
		modChecker := &mockSnapModelChecker{mockError: errors.New("model checker error")}
		return modChecker, nil
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
	c.Assert(err, ErrorMatches, `cannot check if model is authorized to unlock disk: model checker error`)
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

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		activated++
		c.Check(options.RecoveryKeyTries, Equals, 3)
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		_, _, err := keyData.RecoverKeys()
		c.Assert(err, NotNil)
		return nil, sb.ErrRecoveryKeyUsed
	})
	defer restore()

	defaultDevice := "device-name"
	handle := json.RawMessage(`{"a": "handle"}`)
	mockSealedKeyFile := makeMockSealedKeyFile(c, handle)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{AllowRecoveryKey: true}
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

func (s *secbootSuite) checkV2Key(c *C, keyFn string, prefixToDrop, expectedKey, expectedAuxKey []byte, authModel *asserts.Model, handle *json.RawMessage) {
	restore := fde.MockRunFDERevealKey(func(req *fde.RevealKeyRequest) ([]byte, error) {
		c.Check(req.Handle, DeepEquals, handle)
		c.Check(bytes.HasPrefix(req.SealedKey, prefixToDrop), Equals, true)
		payload := req.SealedKey[len(prefixToDrop):]
		return []byte(fmt.Sprintf(`{"key": "%s"}`, base64.StdEncoding.EncodeToString(payload))), nil
	})
	defer restore()

	restore = secboot.MockFDEHasRevealKey(func() bool {
		return true
	})
	defer restore()

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	activated := 0
	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		activated++
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		key, auxKey, err := keyData.RecoverKeys()
		c.Assert(err, IsNil)
		c.Check([]byte(key), DeepEquals, []byte(expectedKey))
		c.Check([]byte(auxKey), DeepEquals, []byte(expectedAuxKey))
		// check against model
		ok, err := keyData.IsSnapModelAuthorized(auxKey, authModel)
		c.Assert(err, IsNil)
		c.Check(ok, Equals, true)
		modChecker := &mockSnapModelChecker{mockIsAuthorized: true}
		return modChecker, nil
	})
	defer restore()

	defaultDevice := "device-name"
	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		WhichModel: func() (*asserts.Model, error) {
			return fakeModel, nil
		},
	}
	res, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, keyFn, opts)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, secboot.UnlockResult{
		UnlockMethod: secboot.UnlockedWithSealedKey,
		IsEncrypted:  true,
		PartDevice:   "/dev/disk/by-partuuid/enc-dev-partuuid",
		FsDevice:     "/dev/mapper/device-name-random-uuid-for-test",
	})
	c.Check(activated, Equals, 1)
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

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	mockEncryptedDiskKey := []byte("USK$encrypted-key-no-json-to-match-denver-project")
	activated := 0
	restore = secboot.MockSbActivateVolumeWithKey(func(volumeName, sourceDevicePath string, key []byte, options *sb.ActivateVolumeOptions) error {
		activated++
		c.Check(key, DeepEquals, mockDiskKey)
		return nil
	})
	defer restore()

	defaultDevice := "device-name"
	// note that we write a v1 created keyfile here, i.e. it's a raw
	// disk-key without any json
	mockSealedKeyFile := filepath.Join(c.MkDir(), "keyfile")
	err := ioutil.WriteFile(mockSealedKeyFile, mockEncryptedDiskKey, 0600)
	c.Assert(err, IsNil)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{}
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
	c.Check(reqs[0].SealedKey, DeepEquals, mockEncryptedDiskKey)
	c.Check(reqs[0].Handle, IsNil)
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

	restore = secboot.MockRandomKernelUUID(func() string {
		return "random-uuid-for-test"
	})
	defer restore()

	mockDiskWithEncDev := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "device-name-enc",
				PartitionUUID:   "enc-dev-partuuid",
			},
		},
	}

	restore = secboot.MockSbActivateVolumeWithKeyData(func(volumeName, sourceDevicePath string, keyData *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error) {
		// XXX: this is what the real
		// MockSbActivateVolumeWithKeyData will do
		_, _, err := keyData.RecoverKeys()
		if err != nil {
			return nil, err
		}
		c.Fatal("should not get this far")
		return nil, nil
	})
	defer restore()

	defaultDevice := "device-name"
	mockSealedKeyFile := makeMockSealedKeyFile(c, nil)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{}
	_, err := secboot.UnlockVolumeUsingSealedKeyIfEncrypted(mockDiskWithEncDev, defaultDevice, mockSealedKeyFile, opts)

	c.Check(err, ErrorMatches, `cannot unlock encrypted partition: invalid key data:.*`)
}
