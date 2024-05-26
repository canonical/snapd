// -*- Mode: Go; indent-tabs-mode: t -*-

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

package boot_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type modelSuite struct {
	baseBootenvSuite

	oldUc20dev snap.Device
	newUc20dev snap.Device

	runKernelBf      bootloader.BootFile
	recoveryKernelBf bootloader.BootFile

	keyID string

	readSystemEssentialCalls int
}

var _ = Suite(&modelSuite{})

var brandPrivKey, _ = assertstest.GenerateKey(752)

func makeEncodableModel(signingAccounts *assertstest.SigningAccounts, overrides map[string]interface{}) *asserts.Model {
	headers := map[string]interface{}{
		"model":        "my-model-uc20",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name": "pc-kernel",
				"id":   "pckernelidididididididididididid",
				"type": "kernel",
			},
			map[string]interface{}{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	for k, v := range overrides {
		headers[k] = v
	}
	return signingAccounts.Model("canonical", headers["model"].(string), headers)
}

func (s *modelSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	store := assertstest.NewStoreStack("canonical", nil)
	brands := assertstest.NewSigningAccounts(store)
	brands.Register("my-brand", brandPrivKey, nil)
	s.keyID = brands.Signing("canonical").KeyID

	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error { return nil })
	s.AddCleanup(restore)
	s.oldUc20dev = boottest.MockUC20Device("", makeEncodableModel(brands, nil))
	s.newUc20dev = boottest.MockUC20Device("", makeEncodableModel(brands, map[string]interface{}{
		"model": "my-new-model-uc20",
		"grade": "secured",
	}))

	model := s.oldUc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// system 1234 corresponds to the new model
		CurrentRecoverySystems: []string{"20200825", "1234"},
		GoodRecoverySystems:    []string{"20200825", "1234"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentKernels: []string{"pc-kernel_500.snap"},
		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := bootloadertest.Mock("trusted", s.bootdir).WithTrustedAssets()
	mtbl.TrustedAssetsMap = map[string]string{"asset": "asset"}
	mtbl.StaticCommandLine = "static cmdline"
	mtbl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		s.runKernelBf,
	}
	mtbl.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		s.recoveryKernelBf,
	}
	bootloader.Force(mtbl)

	s.AddCleanup(func() { bootloader.Force(nil) })

	// run kernel
	s.runKernelBf = bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_500.snap",
		"kernel.efi", bootloader.RoleRunMode)
	// seed (recovery) kernel
	s.recoveryKernelBf = bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
		"kernel.efi", bootloader.RoleRecovery)

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755), IsNil)

	s.readSystemEssentialCalls = 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		s.readSystemEssentialCalls++
		kernelRev := 1
		systemModel := s.oldUc20dev.Model()
		if label == "1234" {
			// recovery system for new model
			kernelRev = 999
			systemModel = s.newUc20dev.Model()
		}
		return systemModel, []*seed.Snap{mockKernelSeedSnap(snap.R(kernelRev)), mockGadgetSeedSnap(c, nil)}, nil
	})
	s.AddCleanup(restore)
}

func (s *modelSuite) TestWriteModelToUbuntuBoot(c *C) {
	mylog.Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")
	mylog.

		// overwrite the file
		Check(boot.WriteModelToUbuntuBoot(s.newUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")
	mylog.Check(os.RemoveAll(filepath.Join(boot.InitramfsUbuntuBootDir)))

	mylog.
		// fails when trying to write
		Check(boot.WriteModelToUbuntuBoot(s.newUc20dev.Model()))
	c.Assert(err, ErrorMatches, `open .*/run/mnt/ubuntu-boot/device/model\..*: no such file or directory`)
}

func (s *modelSuite) TestDeviceChangeHappy(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		switch resealKeysCalls {
		case 1:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2: // recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		case 3:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 4: // recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}

		switch resealKeysCalls {
		case 1, 2:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: dangerous\n")
		case 3, 4:
			// and finally just for the new model
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "/,,")
			// boot/device/model is the new model by this time
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-new-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: secured\n")
		}
		return nil
	})
	defer restore()

	u := mockUnlocker{}
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, u.unlocker))

	c.Assert(resealKeysCalls, Equals, 4)
	c.Check(u.unlocked, Equals, 2)

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeUnhappyFirstReseal(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		switch resealKeysCalls {
		case 1:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}

		switch resealKeysCalls {
		case 1:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: dangerous\n")
		}
		return fmt.Errorf("fail on first try")
	})
	defer restore()
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))
	c.Assert(err, ErrorMatches, "cannot reseal the encryption key: fail on first try")
	c.Assert(resealKeysCalls, Equals, 1)
	// still the old model file
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeUnhappyFirstSwapModelFile(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		switch resealKeysCalls {
		case 1:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}

		switch resealKeysCalls {
		case 1, 2:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: dangerous\n")
		}

		if resealKeysCalls == 2 {
			// break writing of the model file
			c.Assert(os.RemoveAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device")), IsNil)
		}
		return nil
	})
	defer restore()
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))
	c.Assert(err, ErrorMatches, `cannot write new model file: open .*/run/mnt/ubuntu-boot/device/model\..*: no such file or directory`)
	c.Assert(resealKeysCalls, Equals, 2)

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeUnhappySecondReseal(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		// which keys?
		switch resealKeysCalls {
		case 1, 3:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}
		// what's in params?
		switch resealKeysCalls {
		case 1:
			c.Assert(params.ModelParams, HasLen, 2)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[1].Model.Model(), Equals, "my-new-model-uc20")
		case 2:
			// recovery key resealed for current model only
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
		case 3, 4:
			// try model has become current
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-new-model-uc20")
		}
		// what's in modeenv?
		switch resealKeysCalls {
		case 1, 2:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: dangerous\n")
		case 3:
			// and finally just for the new model
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "/,,")
			// boot/device/model is the new model by this time
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-new-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: secured\n")
		}

		if resealKeysCalls == 3 {
			return fmt.Errorf("fail on second try")
		}

		return nil
	})
	defer restore()
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))
	c.Assert(err, ErrorMatches, `cannot reseal the encryption key: fail on second try`)
	c.Assert(resealKeysCalls, Equals, 3)
	// old model file was restored
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeRebootBeforeNewModel(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		c.Logf("reseal key call: %v", resealKeysCalls)
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		// timeline & calls:
		// 1 - pre reboot, run & recovery keys, try model set
		// 2 - pre reboot, recovery keys, try model set, unexpected reboot is triggered
		// (reboot)
		// no call for run key, boot chains haven't changes since call 1
		// 3 - recovery key, try model set
		// 4, 5 - post reboot, run & recovery keys, after rewriting model file, try model cleared

		// which keys?
		switch resealKeysCalls {
		case 1, 4:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2, 3, 5:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}
		// what's in params?
		switch resealKeysCalls {
		case 1:
			c.Assert(params.ModelParams, HasLen, 2)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[1].Model.Model(), Equals, "my-new-model-uc20")
		case 2:
			// attempted reseal of recovery key before clearing try model
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
		case 3:
			// recovery keys are resealed only for current system
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
		case 4, 5:
			// try model has become current
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-new-model-uc20")
		}
		// what's in modeenv?
		switch resealKeysCalls {
		case 1, 2, 3:
			// keys are first resealed for both models, which are restored to the modeenv
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: dangerous\n")
		case 4, 5:
			// and finally just for the new model
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "/,,")
			// boot/device/model is the new model by this time
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-new-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: secured\n")
		}

		if resealKeysCalls == 2 {
			panic("mock reboot after first complete reseal")
		}

		return nil
	})
	defer restore()

	c.Assert(func() { boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil) }, PanicMatches,
		`mock reboot after first complete reseal`)
	c.Assert(resealKeysCalls, Equals, 2)
	// still old model in place
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
	// try model is already set
	c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
	mylog.

		// let's try again
		Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))

	c.Assert(resealKeysCalls, Equals, 5)
	// got new model now
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")

	m = mylog.Check2(boot.ReadModeenv(""))

	currForSealing = boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing = boot.ModelUniqueID(m.TryModelForSealing())
	// new model is current
	c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeRebootAfterNewModelFileWrite(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		c.Logf("reseal key call: %v", resealKeysCalls)
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		// timeline & calls:
		// 1, 2 - pre reboot, run & recovery keys, try model set
		// 3 - run key, after model file has been modified, try model cleared, unexpected
		//     reboot is triggered
		// (reboot)
		// no reseal - boot chains are identical to what was in calls 1 & 2 which were successful
		// 4, 5 - post reboot, run & recovery keys, after rewriting model file, try model cleared

		// which keys?
		switch resealKeysCalls {
		case 1, 3, 4:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2, 5:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}
		// what's in params?
		switch resealKeysCalls {
		case 1:
			c.Assert(params.ModelParams, HasLen, 2)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[1].Model.Model(), Equals, "my-new-model-uc20")
		case 2:
			// recovery key resealed for current model only
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
		case 3:
			// attempted reseal with of run key after clearing try model
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-new-model-uc20")
		case 4, 5:
			// try model has become current
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-new-model-uc20")
		}
		// what's in modeenv?
		switch resealKeysCalls {
		case 1, 2:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
		case 3, 4, 5:
			// and finally just for the new model
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "/,,")
			// boot/device/model is the new model by this time
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-new-model-uc20\n")
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"grade: secured\n")
		}

		if resealKeysCalls == 3 {
			panic("mock reboot before second complete reseal")
		}

		return nil
	})
	defer restore()

	c.Assert(func() { boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil) }, PanicMatches,
		`mock reboot before second complete reseal`)
	c.Assert(resealKeysCalls, Equals, 3)
	// model file has already been replaced
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	// as well as modeenv
	c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
	c.Assert(tryForSealing, Equals, "/,,")
	mylog.

		// let's try again (post reboot)
		Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))

	c.Assert(resealKeysCalls, Equals, 5)
	// got new model now
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")

	m = mylog.Check2(boot.ReadModeenv(""))

	currForSealing = boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing = boot.ModelUniqueID(m.TryModelForSealing())
	// new model is current
	c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeRebootPostSameModel(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		c.Logf("reseal key call: %v", resealKeysCalls)
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		// timeline & calls:
		// 1, 2 - pre reboot, run & recovery keys, try model set
		// 3 - run key, after model file has been modified, try model cleared
		// 4 - recovery key, model file has been modified, try model cleared, unexpected
		//     reboot is triggered
		// (reboot)
		// 5, 6 - run & recovery, try model set, new model also restored
		//        as 'old' model, params are grouped by model
		// 7 - run only (recovery boot chains have not changed since)

		// which keys?
		switch resealKeysCalls {
		case 1, 3, 5, 7:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2, 4, 6:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}
		// what's in params?
		switch resealKeysCalls {
		case 1:
			c.Assert(params.ModelParams, HasLen, 2)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[1].Model.Model(), Equals, "my-new-model-uc20")
		case 2:
			// recovery key resealed for current model only
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
		case 3, 4:
			// try model has become current
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-new-model-uc20")
		case 5, 6, 7:
			//
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-new-model-uc20")
		}
		// what's in modeenv?
		switch resealKeysCalls {
		case 1, 2:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-model-uc20\n")
		case 3, 4, 7:
			// and finally just for the new model
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "/,,")
			// boot/device/model is the new model by this time
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-new-model-uc20\n")
		case 5, 6:
			// new model passed as old one
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// boot/device/model is still the old file
			c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
				"model: my-new-model-uc20\n")
		}

		if resealKeysCalls == 4 {
			panic("mock reboot before second complete reseal")
		}
		return nil
	})
	defer restore()

	// as if called by device manager in task handler
	c.Assert(func() { boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil) }, PanicMatches,
		`mock reboot before second complete reseal`)
	c.Assert(resealKeysCalls, Equals, 4)
	// model file has already been replaced
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")
	mylog.

		// as if called by device manager, after the model has been changed, but
		// the set-model task isn't marked as done
		Check(boot.DeviceChange(s.newUc20dev, s.newUc20dev, nil))

	c.Assert(resealKeysCalls, Equals, 7)

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-new-model-uc20\n")

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

type unhappyMockedWriteModelToBootTestCase struct {
	breakModeenvAfterFirstWrite bool
	modelRestoreFail            bool
	expectedErr                 string
}

func (s *modelSuite) testDeviceChangeUnhappyMockedWriteModelToBoot(c *C, tc unhappyMockedWriteModelToBootTestCase) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	modeenvDir := filepath.Dir(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))
	defer os.Chmod(modeenvDir, 0755)

	writeModelToBootCalls := 0
	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		switch resealKeysCalls {
		case 1:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}

		switch resealKeysCalls {
		case 1:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			// no model has been written to ubuntu-boot yet
			c.Assert(writeModelToBootCalls, Equals, 0)
		}
		return nil
	})
	defer restore()

	restore = boot.MockWriteModelToUbuntuBoot(func(model *asserts.Model) error {
		writeModelToBootCalls++
		c.Assert(model, NotNil)
		switch writeModelToBootCalls {
		case 1:
			// a call to write the new model
			c.Check(model.Model(), Equals, "my-new-model-uc20")
			// only 2 calls to reseal until now
			c.Check(resealKeysCalls, Equals, 2)
			if tc.breakModeenvAfterFirstWrite {
				c.Assert(os.Chmod(modeenvDir, 0000), IsNil)
				return nil
			}
		case 2:
			// a call to restore the old model
			c.Check(model.Model(), Equals, "my-model-uc20")
			if !tc.breakModeenvAfterFirstWrite {
				c.Errorf("unexpected additional call to writeModelToBoot (call # %d)", writeModelToBootCalls)
			}
			if !tc.modelRestoreFail {
				return nil
			}
		default:
			c.Errorf("unexpected additional call to writeModelToBoot (call # %d)", writeModelToBootCalls)
		}
		return fmt.Errorf("mocked fail in write model to boot")
	})
	defer restore()
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))
	c.Assert(err, ErrorMatches, tc.expectedErr)
	c.Assert(resealKeysCalls, Equals, 2)
	if tc.breakModeenvAfterFirstWrite {
		// write to boot failed on the second call
		c.Assert(writeModelToBootCalls, Equals, 2)
	} else {
		c.Assert(writeModelToBootCalls, Equals, 1)
	}
	// still the old model file, all writes were intercepted
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	if !tc.breakModeenvAfterFirstWrite {
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
		// try model has been cleared
		c.Assert(tryForSealing, Equals, "/,,")
	}
}

func (s *modelSuite) TestDeviceChangeUnhappyMockedWriteModelToBootBeforeModelSwap(c *C) {
	s.testDeviceChangeUnhappyMockedWriteModelToBoot(c, unhappyMockedWriteModelToBootTestCase{
		expectedErr: "cannot write new model file: mocked fail in write model to boot",
	})
}

func (s *modelSuite) TestDeviceChangeUnhappyMockedWriteModelToBootAfterModelSwapFailingRestore(c *C) {
	// writing modeenv after placing new model file on disk fails, and so
	// does restoring of the old model
	if os.Getuid() == 0 {
		// the test is manipulating file permissions, which doesn't
		// affect root
		c.Skip("test cannot be executed by root")
	}
	s.testDeviceChangeUnhappyMockedWriteModelToBoot(c, unhappyMockedWriteModelToBootTestCase{
		breakModeenvAfterFirstWrite: true,
		modelRestoreFail:            true,

		expectedErr: `open .*/var/lib/snapd/modeenv\..*: permission denied \(restoring model failed: mocked fail in write model to boot\)`,
	})
}

func (s *modelSuite) TestDeviceChangeUnhappyMockedWriteModelToBootAfterModelSwapHappyRestore(c *C) {
	// writing modeenv after placing new model file on disk fails, but
	// restore is successful
	if os.Getuid() == 0 {
		// the test is manipulating file permissions, which doesn't
		// affect root
		c.Skip("test cannot be executed by root")
	}
	s.testDeviceChangeUnhappyMockedWriteModelToBoot(c, unhappyMockedWriteModelToBootTestCase{
		breakModeenvAfterFirstWrite: true,
		modelRestoreFail:            false,

		expectedErr: `open .*/var/lib/snapd/modeenv\..*: permission denied$`,
	})
}

func (s *modelSuite) TestDeviceChangeUnhappyFailReseaWithSwappedModelMockedWriteToBoot(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)
	mylog.

		// set up the old model file
		Check(boot.WriteModelToUbuntuBoot(s.oldUc20dev.Model()))

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	writeModelToBootCalls := 0
	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		if resealKeysCalls == 3 {
			// we are resealing the run key, the old model has been
			// replaced by the new one in modeenv
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
			m := mylog.Check2(boot.ReadModeenv(""))

			currForSealing := boot.ModelUniqueID(m.ModelForSealing())
			tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)
			c.Assert(tryForSealing, Equals, "/,,")
			// an new model has already been written
			c.Assert(writeModelToBootCalls, Equals, 1)
			return fmt.Errorf("mock reseal failure")
		}

		return nil
	})
	defer restore()

	restore = boot.MockWriteModelToUbuntuBoot(func(model *asserts.Model) error {
		writeModelToBootCalls++
		switch writeModelToBootCalls {
		case 1:
			c.Assert(model, NotNil)
			c.Check(model.Model(), Equals, "my-new-model-uc20")
			// only 2 calls to reseal until now
			c.Check(resealKeysCalls, Equals, 2)
		case 2:
			// handling of reseal with new model restores the old one on the disk
			c.Check(model.Model(), Equals, "my-model-uc20")
			m := mylog.Check2(boot.ReadModeenv(""))

			// and both models are present in the modeenv
			currForSealing := boot.ModelUniqueID(m.ModelForSealing())
			tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
			c.Assert(tryForSealing, Equals, "canonical/my-new-model-uc20,secured,"+s.keyID)

		default:
			c.Errorf("unexpected additional call to writeModelToBoot (call # %d)", writeModelToBootCalls)
		}
		return nil
	})
	defer restore()
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))
	c.Assert(err, ErrorMatches, `cannot reseal the encryption key: mock reseal failure`)
	c.Assert(resealKeysCalls, Equals, 3)
	c.Assert(writeModelToBootCalls, Equals, 2)
	// still the old model file, all writes were intercepted
	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileContains,
		"model: my-model-uc20\n")

	// finally the try model has been dropped from modeenv
	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "canonical/my-model-uc20,dangerous,"+s.keyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}

func (s *modelSuite) TestDeviceChangeRebootRestoreModelKeyChangeMockedWriteModel(c *C) {
	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	oldKeyID := "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij"
	newKeyID := "ZZZ_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij"
	// model can be mocked freely as we will not encode it as we mocked a
	// function that writes out the model too
	s.oldUc20dev = boottest.MockUC20Device("", boottest.MakeMockUC20Model(map[string]interface{}{
		"model":             "my-model-uc20",
		"brand-id":          "my-brand",
		"grade":             "dangerous",
		"sign-key-sha3-384": oldKeyID,
	}))

	s.newUc20dev = boottest.MockUC20Device("", boottest.MakeMockUC20Model(map[string]interface{}{
		"model":             "my-model-uc20",
		"brand-id":          "my-brand",
		"grade":             "dangerous",
		"sign-key-sha3-384": newKeyID,
	}))

	resealKeysCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		c.Logf("reseal key call: %v", resealKeysCalls)
		m := mylog.Check2(boot.ReadModeenv(""))

		currForSealing := boot.ModelUniqueID(m.ModelForSealing())
		tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
		// timeline & calls:
		// 1, 2 - pre reboot, run & recovery keys, try model set
		// 3 - run key, after model file has been modified, try model cleared
		// 4 - recovery key, model file has been modified, try model cleared,
		//     unexpected reboot is triggered
		// (reboot)
		// 5 - run with old model & key (since we resealed run key in
		//     call 3, and recovery has not changed), old model restored in modeenv
		// 6 - run with new model and key, old current has been dropped
		// 7 - recovery with new model only

		// which keys?
		switch resealKeysCalls {
		case 1, 3, 5, 6:
			// run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
		case 2, 4, 7:
			// recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}
		// what's in params?
		switch resealKeysCalls {
		case 1, 5:
			c.Assert(params.ModelParams, HasLen, 2)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[0].Model.SignKeyID(), Equals, oldKeyID)
			c.Assert(params.ModelParams[1].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[1].Model.SignKeyID(), Equals, newKeyID)
		case 2:
			// recovery key resealed for current model only
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[0].Model.SignKeyID(), Equals, oldKeyID)
		case 3, 4, 6, 7:
			// try model has become current
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[0].Model.SignKeyID(), Equals, newKeyID)
		}
		// what's in modeenv?
		switch resealKeysCalls {
		case 1, 2, 5:
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "my-brand/my-model-uc20,dangerous,"+oldKeyID)
			c.Assert(tryForSealing, Equals, "my-brand/my-model-uc20,dangerous,"+newKeyID)
		case 3, 4, 6, 7:
			// and finally just for the new model
			c.Assert(currForSealing, Equals, "my-brand/my-model-uc20,dangerous,"+newKeyID)
			c.Assert(tryForSealing, Equals, "/,,")
		}

		if resealKeysCalls == 4 {
			panic("mock reboot before second complete reseal")
		}
		return nil
	})
	defer restore()

	writeModelToBootCalls := 0
	restore = boot.MockWriteModelToUbuntuBoot(func(model *asserts.Model) error {
		writeModelToBootCalls++
		c.Logf("write model to boot call: %v", writeModelToBootCalls)
		switch writeModelToBootCalls {
		case 1:
			c.Assert(model, NotNil)
			c.Check(model.Model(), Equals, "my-model-uc20")
			// only 2 calls to reseal until now
			c.Check(resealKeysCalls, Equals, 2)
		case 2:
			// handling of reseal with new model restores the old one on the disk
			c.Check(model.Model(), Equals, "my-model-uc20")
			m := mylog.Check2(boot.ReadModeenv(""))

			// and both models are present in the modeenv
			currForSealing := boot.ModelUniqueID(m.ModelForSealing())
			tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
			// keys are first resealed for both models
			c.Assert(currForSealing, Equals, "my-brand/my-model-uc20,dangerous,"+oldKeyID)
			c.Assert(tryForSealing, Equals, "my-brand/my-model-uc20,dangerous,"+newKeyID)

		default:
			c.Errorf("unexpected additional call to writeModelToBoot (call # %d)", writeModelToBootCalls)
		}
		return nil
	})
	defer restore()

	// as if called by device manager in task handler
	c.Assert(func() { boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil) }, PanicMatches,
		`mock reboot before second complete reseal`)
	c.Assert(resealKeysCalls, Equals, 4)
	c.Assert(writeModelToBootCalls, Equals, 1)
	mylog.Check(boot.DeviceChange(s.oldUc20dev, s.newUc20dev, nil))

	c.Assert(resealKeysCalls, Equals, 7)
	c.Assert(writeModelToBootCalls, Equals, 2)

	m := mylog.Check2(boot.ReadModeenv(""))

	currForSealing := boot.ModelUniqueID(m.ModelForSealing())
	tryForSealing := boot.ModelUniqueID(m.TryModelForSealing())
	c.Assert(currForSealing, Equals, "my-brand/my-model-uc20,dangerous,"+newKeyID)
	// try model has been cleared
	c.Assert(tryForSealing, Equals, "/,,")
}
