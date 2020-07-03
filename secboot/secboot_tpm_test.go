// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/canonical/go-tpm2"
	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

func TestSecboot(t *testing.T) { TestingT(t) }

type secbootSuite struct {
	testutil.BaseTest
}

var _ = Suite(&secbootSuite{})

func (s *secbootSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *secbootSuite) TestCheckKeySealingSupported(c *C) {
	sbEmpty := []uint8{}
	sbEnabled := []uint8{1}
	sbDisabled := []uint8{0}
	efiNotSupported := []uint8(nil)

	type testCase struct {
		hasTPM bool
		sbData []uint8
		errStr string
	}
	for _, t := range []testCase{
		{true, sbEnabled, ""},
		{true, sbEmpty, "secure boot variable does not exist"},
		{false, sbEmpty, "secure boot variable does not exist"},
		{true, sbDisabled, "secure boot is disabled"},
		{false, sbEnabled, "cannot connect to TPM device: TPM not available"},
		{false, sbDisabled, "secure boot is disabled"},
		{true, efiNotSupported, "not a supported EFI system"},
	} {
		c.Logf("t: %v %v %q", t.hasTPM, t.sbData, t.errStr)

		restore := secboot.MockSbConnectToDefaultTPM(func() (*sb.TPMConnection, error) {
			if !t.hasTPM {
				return nil, errors.New("TPM not available")
			}
			tcti, err := os.Open("/dev/null")
			c.Assert(err, IsNil)
			tpm, err := tpm2.NewTPMContext(tcti)
			c.Assert(err, IsNil)
			mockTPM := &sb.TPMConnection{TPMContext: tpm}
			return mockTPM, nil
		})
		defer restore()

		var vars map[string][]byte
		if t.sbData != nil {
			vars = map[string][]byte{"SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c": t.sbData}
		}
		restoreEfiVars := efi.MockVars(vars, nil)
		defer restoreEfiVars()

		err := secboot.CheckKeySealingSupported()
		if t.errStr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, t.errStr)
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
			tpmErr: sb.ErrNoTPM2Device,
		},
	} {
		mockTpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb.TPMConnection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		calls := 0
		restore = secboot.MockSbMeasureSnapSystemEpochToTPM(func(tpm *sb.TPMConnection, pcrIndex int) error {
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
			tpmErr: sb.ErrNoTPM2Device,
		},
	} {
		c.Logf("%d: tpmErr:%v tpmEnabled:%v", i, tc.tpmErr, tc.tpmEnabled)
		mockModel := &asserts.Model{}

		mockTpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb.TPMConnection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		calls := 0
		restore = secboot.MockSbMeasureSnapModelToTPM(func(tpm *sb.TPMConnection, pcrIndex int, model *asserts.Model) error {
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

func (s *secbootSuite) TestUnlockIfEncrypted(c *C) {
	for idx, tc := range []struct {
		tpmErr      error
		tpmEnabled  bool  // TPM storage and endorsement hierarchies disabled, only relevant if TPM available
		hasEncdev   bool  // an encrypted device exists
		rkErr       error // recovery key unlock error, only relevant if TPM not available
		lockRequest bool  // request to lock access to the sealed key, only relevant if TPM available
		lockOk      bool  // the lock operation succeeded
		activated   bool  // the activation operation succeeded
		device      string
		err         string
	}{
		{
			// happy case with tpm and encrypted device (lock requested)
			tpmEnabled: true, hasEncdev: true, lockRequest: true, lockOk: true,
			activated: true, device: "name",
		}, {
			// device activation fails (lock requested)
			tpmEnabled: true, hasEncdev: true, lockRequest: true, lockOk: true,
			err: "cannot activate encrypted device .*: activation error",
		}, {
			// activation works but lock fails (lock requested)
			tpmEnabled: true, hasEncdev: true, lockRequest: true, activated: true,
			err: "cannot lock access to sealed keys: lock failed",
		}, {
			// happy case with tpm and encrypted device
			tpmEnabled: true, hasEncdev: true, lockOk: true, activated: true,
			device: "name",
		}, {
			// device activation fails
			tpmEnabled: true, hasEncdev: true,
			err: "cannot activate encrypted device .*: activation error",
		}, {
			// activation works but lock fails
			tpmEnabled: true, hasEncdev: true, activated: true, device: "name",
		}, {
			// happy case without encrypted device (lock requested)
			tpmEnabled: true, lockRequest: true, lockOk: true, activated: true,
			device: "name",
		}, {
			// activation works but lock fails, without encrypted device (lock requested)
			tpmEnabled: true, lockRequest: true, activated: true,
			err: "cannot lock access to sealed keys: lock failed",
		}, {
			// happy case without encrypted device
			tpmEnabled: true, lockOk: true, activated: true, device: "name",
		}, {
			// activation works but lock fails, no encrypted device
			tpmEnabled: true, activated: true, device: "name",
		}, {
			// tpm error, no encrypted device
			tpmErr: errors.New("tpm error"),
			err:    `cannot unlock encrypted device "name": tpm error`,
		}, {
			// tpm error, has encrypted device
			tpmErr: errors.New("tpm error"), hasEncdev: true,
			err: `cannot unlock encrypted device "name": tpm error`,
		}, {
			// tpm disabled, no encrypted device
			device: "name",
		}, {
			// tpm disabled, has encrypted device, unlocked using the recovery key
			hasEncdev: true,
			device:    "name",
		}, {
			// tpm disabled, has encrypted device, recovery key unlocking fails
			hasEncdev: true, rkErr: errors.New("cannot unlock with recovery key"),
			err: `cannot unlock encrypted device ".*/name-enc": cannot unlock with recovery key`,
		}, {
			// no tpm, has encrypted device, unlocked using the recovery key (lock requested)
			tpmErr: sb.ErrNoTPM2Device, hasEncdev: true, lockRequest: true,
			device: "name",
		}, {
			// no tpm, has encrypted device, recovery key unlocking fails
			rkErr:  errors.New("cannot unlock with recovery key"),
			tpmErr: sb.ErrNoTPM2Device, hasEncdev: true, lockRequest: true,
			err: `cannot unlock encrypted device ".*/name-enc": cannot unlock with recovery key`,
		}, {
			// no tpm, has encrypted device, unlocked using the recovery key
			tpmErr: sb.ErrNoTPM2Device, hasEncdev: true,
			device: "name",
		}, {
			// no tpm, no encrypted device (lock requested)
			tpmErr: sb.ErrNoTPM2Device, lockRequest: true,
			device: "name",
		}, {
			// no tpm, no encrypted device
			tpmErr: sb.ErrNoTPM2Device,
			device: "name",
		},
	} {
		randomUUID := fmt.Sprintf("random-uuid-for-test-%d", idx)
		restore := secboot.MockRandomKernelUUID(func() string {
			return randomUUID
		})
		defer restore()

		c.Logf("tc %v: %+v", idx, tc)
		mockSbTPM, restoreConnect := mockSbTPMConnection(c, tc.tpmErr)
		defer restoreConnect()

		restore = secboot.MockIsTPMEnabled(func(tpm *sb.TPMConnection) bool {
			return tc.tpmEnabled
		})
		defer restore()

		n := 0
		restore = secboot.MockSbLockAccessToSealedKeys(func(tpm *sb.TPMConnection) error {
			n++
			c.Assert(tpm, Equals, mockSbTPM)
			if tc.lockOk {
				return nil
			}
			return errors.New("lock failed")
		})
		defer restore()

		devDiskByLabel, restoreDev := mockDevDiskByLabel(c)
		defer restoreDev()
		if tc.hasEncdev {
			err := ioutil.WriteFile(filepath.Join(devDiskByLabel, "name-enc"), nil, 0644)
			c.Assert(err, IsNil)
		}

		restore = secboot.MockSbActivateVolumeWithTPMSealedKey(func(tpm *sb.TPMConnection, volumeName, sourceDevicePath,
			keyPath string, pinReader io.Reader, options *sb.ActivateWithTPMSealedKeyOptions) (bool, error) {
			c.Assert(volumeName, Equals, "name-"+randomUUID)
			c.Assert(sourceDevicePath, Equals, filepath.Join(devDiskByLabel, "name-enc"))
			c.Assert(keyPath, Equals, filepath.Join(boot.InitramfsEncryptionKeyDir, "name.sealed-key"))
			c.Assert(*options, DeepEquals, sb.ActivateWithTPMSealedKeyOptions{
				PINTries:            1,
				RecoveryKeyTries:    3,
				LockSealedKeyAccess: tc.lockRequest,
			})
			if !tc.activated {
				return false, errors.New("activation error")
			}
			return true, nil
		})
		defer restore()

		restore = secboot.MockSbActivateVolumeWithRecoveryKey(func(name, device string, keyReader io.Reader,
			options *sb.ActivateWithRecoveryKeyOptions) error {
			return tc.rkErr
		})
		defer restore()

		device, err := secboot.UnlockVolumeIfEncrypted("name", tc.lockRequest)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
		if tc.device == "" {
			c.Assert(device, Equals, tc.device)
		} else {
			if tc.hasEncdev {
				c.Assert(device, Equals, filepath.Join("/dev/mapper", tc.device+"-"+randomUUID))
			} else {
				c.Assert(device, Equals, filepath.Join(devDiskByLabel, tc.device))
			}
		}
		// LockAccessToSealedKeys should be called whenever there is a TPM device
		// detected, regardless of whether secure boot is enabled or there is an
		// encrypted volume to unlock. If we have multiple encrypted volumes, we
		// should call it after the last one is unlocked.
		if tc.tpmErr == nil && tc.lockRequest {
			c.Assert(n, Equals, 1)
		} else {
			c.Assert(n, Equals, 0)
		}
	}
}

func (s *secbootSuite) TestSealKey(c *C) {
	tmpDir := c.MkDir()

	var mockEFI []string
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		mockFileName := filepath.Join(tmpDir, name)
		err := ioutil.WriteFile(mockFileName, nil, 0644)
		c.Assert(err, IsNil)
		mockEFI = append(mockEFI, mockFileName)
	}

	myParams := secboot.SealKeyParams{
		ModelParams: []*secboot.SealKeyModelParams{
			{
				EFILoadChains:  [][]string{{mockEFI[0], mockEFI[1], mockEFI[2], mockEFI[3]}},
				KernelCmdlines: []string{"cmdline1"},
				Model:          &asserts.Model{},
			},
			{
				EFILoadChains:  [][]string{{mockEFI[0], mockEFI[1], mockEFI[2]}, {mockEFI[3], mockEFI[4]}},
				KernelCmdlines: []string{"cmdline2", "cmdline3"},
				Model:          &asserts.Model{},
			},
		},
		KeyFile:                 "keyfile",
		TPMPolicyUpdateDataFile: "policy-update-data-file",
		TPMLockoutAuthFile:      filepath.Join(tmpDir, "lockout-auth-file"),
	}

	myKey := secboot.EncryptionKey{}
	for i := range myKey {
		myKey[i] = byte(i)
	}

	sequences1 := []*sb.EFIImageLoadEvent{
		{
			Source: sb.Firmware,
			Image:  sb.FileEFIImage(mockEFI[0]),
			Next: []*sb.EFIImageLoadEvent{
				{
					Source: sb.Shim,
					Image:  sb.FileEFIImage(mockEFI[1]),
					Next: []*sb.EFIImageLoadEvent{
						{
							Source: sb.Shim,
							Image:  sb.FileEFIImage(mockEFI[2]),
							Next: []*sb.EFIImageLoadEvent{
								{
									Source: sb.Shim,
									Image:  sb.FileEFIImage(mockEFI[3]),
								},
							},
						},
					},
				},
			},
		},
	}

	sequences2 := []*sb.EFIImageLoadEvent{
		{
			Source: sb.Firmware,
			Image:  sb.FileEFIImage(mockEFI[0]),
			Next: []*sb.EFIImageLoadEvent{
				{
					Source: sb.Shim,
					Image:  sb.FileEFIImage(mockEFI[1]),
					Next: []*sb.EFIImageLoadEvent{
						{
							Source: sb.Shim,
							Image:  sb.FileEFIImage(mockEFI[2]),
						},
					},
				},
			},
		},
		{
			Source: sb.Firmware,
			Image:  sb.FileEFIImage(mockEFI[3]),
			Next: []*sb.EFIImageLoadEvent{
				{
					Source: sb.Shim,
					Image:  sb.FileEFIImage(mockEFI[4]),
				},
			},
		},
	}

	for _, tc := range []struct {
		tpmErr               error
		addProfileCallNum    int
		provisionSealCallNum int
		err                  string
	}{
		{tpmErr: errors.New("tpm error"), addProfileCallNum: 0, provisionSealCallNum: 0, err: "cannot connect to TPM: tpm error"},
		{tpmErr: nil, addProfileCallNum: 2, provisionSealCallNum: 1, err: ""},
	} {
		tpm, restore := mockSbTPMConnection(c, tc.tpmErr)
		defer restore()

		// mock adding EFI secure boot policy profile
		var pcrProfile *sb.PCRProtectionProfile
		addEFISbPolicyCalls := 0
		restore = secboot.MockSbAddEFISecureBootPolicyProfile(func(profile *sb.PCRProtectionProfile, params *sb.EFISecureBootPolicyProfileParams) error {
			addEFISbPolicyCalls++
			pcrProfile = profile
			c.Assert(params.PCRAlgorithm, Equals, tpm2.HashAlgorithmSHA256)
			switch addEFISbPolicyCalls {
			case 1:
				c.Assert(params.LoadSequences, DeepEquals, sequences1)
			case 2:
				c.Assert(params.LoadSequences, DeepEquals, sequences2)
			default:
				c.Error("AddEFISecureBootPolicyProfile shouldn't be called a third time")
			}
			return nil
		})
		defer restore()

		// mock adding systemd EFI stub profile
		addSystemdEfiStubCalls := 0
		restore = secboot.MockSbAddSystemdEFIStubProfile(func(profile *sb.PCRProtectionProfile, params *sb.SystemdEFIStubProfileParams) error {
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
				c.Error("AddSystemdEFIStubProfile shouldn't be called a third time")
			}
			return nil
		})
		defer restore()

		// mock adding snap model profile
		addSnapModelCalls := 0
		restore = secboot.MockSbAddSnapModelProfile(func(profile *sb.PCRProtectionProfile, params *sb.SnapModelProfileParams) error {
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
			return nil
		})
		defer restore()

		// mock provisioning
		provisioningCalls := 0
		restore = secboot.MockSbProvisionTPM(func(t *sb.TPMConnection, mode sb.ProvisionMode, newLockoutAuth []byte) error {
			provisioningCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(mode, Equals, sb.ProvisionModeFull)
			c.Assert(myParams.TPMLockoutAuthFile, testutil.FilePresent)
			return nil
		})
		defer restore()

		// mock sealing
		sealCalls := 0
		restore = secboot.MockSbSealKeyToTPM(func(t *sb.TPMConnection, key []byte, keyPath, policyUpdatePath string, params *sb.KeyCreationParams) error {
			sealCalls++
			c.Assert(t, Equals, tpm)
			c.Assert(key, DeepEquals, myKey[:])
			c.Assert(keyPath, Equals, myParams.KeyFile)
			c.Assert(policyUpdatePath, Equals, myParams.TPMPolicyUpdateDataFile)
			c.Assert(params.PINHandle, Equals, tpm2.Handle(0x01880000))

			return nil
		})
		defer restore()

		err := secboot.SealKey(myKey, &myParams)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
		c.Assert(addEFISbPolicyCalls, Equals, tc.addProfileCallNum)
		c.Assert(addSystemdEfiStubCalls, Equals, tc.addProfileCallNum)
		c.Assert(addSnapModelCalls, Equals, tc.addProfileCallNum)
		c.Assert(provisioningCalls, Equals, tc.provisionSealCallNum)
		c.Assert(sealCalls, Equals, tc.provisionSealCallNum)

	}
}

func (s *secbootSuite) TestSealKeyNoModelParams(c *C) {
	myKey := secboot.EncryptionKey{}
	myParams := secboot.SealKeyParams{
		KeyFile:                 "keyfile",
		TPMPolicyUpdateDataFile: "policy-update-data-file",
		TPMLockoutAuthFile:      "lockout-auth-file",
	}

	err := secboot.SealKey(myKey, &myParams)
	c.Assert(err, ErrorMatches, "at least one set of model-specific parameters is required")
}

func mockSbTPMConnection(c *C, tpmErr error) (*sb.TPMConnection, func()) {
	tcti, err := os.Open("/dev/null")
	c.Assert(err, IsNil)
	tpmctx, err := tpm2.NewTPMContext(tcti)
	c.Assert(err, IsNil)
	tpm := &sb.TPMConnection{TPMContext: tpmctx}
	restore := secboot.MockSbConnectToDefaultTPM(func() (*sb.TPMConnection, error) {
		if tpmErr != nil {
			return nil, tpmErr
		}
		return tpm, nil
	})
	return tpm, restore
}

func mockDevDiskByLabel(c *C) (string, func()) {
	devDir := filepath.Join(c.MkDir(), "dev/disk/by-label")
	err := os.MkdirAll(devDir, 0755)
	c.Assert(err, IsNil)
	restore := secboot.MockDevDiskByLabelDir(devDir)
	return devDir, restore
}
