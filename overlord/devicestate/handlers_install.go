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

package devicestate

import (
	"bytes"
	"compress/gzip"
	"crypto"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	_ "golang.org/x/crypto/sha3"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	installLogic "github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
)

var (
	bootMakeBootablePartition            = boot.MakeBootablePartition
	bootMakeRunnable                     = boot.MakeRunnableSystem
	bootMakeRunnableStandalone           = boot.MakeRunnableStandaloneSystem
	bootMakeRunnableAfterDataReset       = boot.MakeRunnableSystemAfterDataReset
	bootEnsureNextBootToRunMode          = boot.EnsureNextBootToRunMode
	installRun                           = install.Run
	installFactoryReset                  = install.FactoryReset
	installMountVolumes                  = install.MountVolumes
	installWriteContent                  = install.WriteContent
	installEncryptPartitions             = install.EncryptPartitions
	installSaveStorageTraits             = install.SaveStorageTraits
	installMatchDisksToGadgetVolumes     = install.MatchDisksToGadgetVolumes
	secbootStageEncryptionKeyChange      = secboot.StageEncryptionKeyChange
	secbootTransitionEncryptionKeyChange = secboot.TransitionEncryptionKeyChange

	installLogicPrepareRunSystemData = installLogic.PrepareRunSystemData
)

func writeLogs(rootdir string, fromMode string) error {
	// XXX: would be great to use native journal format but it's tied
	//      to machine-id, we could journal -o export but there
	//      is no systemd-journal-remote on core{,18,20}
	//
	// XXX: or only log if persistent journal is enabled?
	logPath := filepath.Join(rootdir, "var/log/install-mode.log.gz")
	if fromMode == "factory-reset" {
		logPath = filepath.Join(rootdir, "var/log/factory-reset-mode.log.gz")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	cmd := exec.Command("journalctl", "-b", "0", "--all")
	cmd.Stdout = gz
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot collect journal output: %v", err)
	}
	if err := gz.Flush(); err != nil {
		return fmt.Errorf("cannot flush compressed log output: %v", err)
	}

	return nil
}

func writeTimings(st *state.State, rootdir, fromMode string) error {
	changeKind := "install-system"
	logPath := filepath.Join(rootdir, "var/log/install-timings.txt.gz")
	if fromMode == "factory-reset" {
		changeKind = "factory-reset"
		logPath = filepath.Join(rootdir, "var/log/factory-reset-timings.txt.gz")
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	var chgIDs []string
	for _, chg := range st.Changes() {
		if chg.Kind() == "seed" || chg.Kind() == changeKind {
			// this is captured via "--ensure=seed" and
			// "--ensure=install-system" below
			continue
		}
		chgIDs = append(chgIDs, chg.ID())
	}

	// state must be unlocked for "snap changes/debug timings" to work
	st.Unlock()
	defer st.Lock()

	// XXX: ugly, ugly, but using the internal timings requires
	//      some refactor as a lot of the required bits are not
	//      exported right now
	// first all changes
	fmt.Fprintf(gz, "---- Output of: snap changes\n")
	cmd := exec.Command("snap", "changes")
	cmd.Stdout = gz
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot collect timings output: %v", err)
	}
	fmt.Fprintf(gz, "\n")
	// then the seeding
	fmt.Fprintf(gz, "---- Output of snap debug timings --ensure=seed\n")
	cmd = exec.Command("snap", "debug", "timings", "--ensure=seed")
	cmd.Stdout = gz
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot collect timings output: %v", err)
	}
	fmt.Fprintf(gz, "\n")
	// then the install
	fmt.Fprintf(gz, "---- Output of snap debug timings --ensure=%v\n", changeKind)
	cmd = exec.Command("snap", "debug", "timings", fmt.Sprintf("--ensure=%v", changeKind))
	cmd.Stdout = gz
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot collect timings output: %v", err)
	}
	// then the other changes (if there are any)
	for _, chgID := range chgIDs {
		fmt.Fprintf(gz, "---- Output of snap debug timings %s\n", chgID)
		cmd = exec.Command("snap", "debug", "timings", chgID)
		cmd.Stdout = gz
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cannot collect timings output: %v", err)
		}
		fmt.Fprintf(gz, "\n")
	}

	if err := gz.Flush(); err != nil {
		return fmt.Errorf("cannot flush timings output: %v", err)
	}

	return nil
}

func (m *DeviceManager) doSetupUbuntuSave(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}

	return m.setupUbuntuSave(deviceCtx)
}

func (m *DeviceManager) doSetupRunSystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	// get gadget dir
	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}
	gadgetInfo, err := snapstate.GadgetInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get gadget info: %v", err)
	}
	gadgetDir := gadgetInfo.MountDir()

	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get kernel info: %v", err)
	}
	kernelDir := kernelInfo.MountDir()

	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}
	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	// bootstrap
	bopts := install.Options{
		Mount: true,
	}
	encryptionType, err := m.checkEncryption(st, deviceCtx, secboot.TPMProvisionFull)
	if err != nil {
		return err
	}
	bopts.EncryptionType = encryptionType
	useEncryption := (encryptionType != secboot.EncryptionTypeNone)

	model := deviceCtx.Model()

	// make sure that gadget is usable for the set up we want to use it in
	validationConstraints := gadget.ValidationConstraints{
		EncryptedData: useEncryption,
	}
	var ginfo *gadget.Info
	timings.Run(perfTimings, "read-info-and-validate", "Read and validate gagdet info", func(timings.Measurer) {
		ginfo, err = gadget.ReadInfoAndValidate(gadgetDir, model, &validationConstraints)
	})
	if err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}
	if err := gadget.ValidateContent(ginfo, gadgetDir, kernelDir); err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}

	installObserver, trustedInstallObserver, err := installLogic.BuildInstallObserver(model, gadgetDir, useEncryption)
	if err != nil {
		return err
	}

	var installedSystem *install.InstalledSystemSideData
	// run the create partition code
	logger.Noticef("create and deploy partitions")
	kSnapInfo := &install.KernelSnapInfo{
		Name:       kernelInfo.SnapName(),
		MountPoint: kernelDir,
		Revision:   kernelInfo.Revision,
		IsCore:     !deviceCtx.Classic(),
	}
	if snapstate.NeedsKernelSetup(deviceCtx) {
		kSnapInfo.NeedsDriversTree = true
	}
	timings.Run(perfTimings, "install-run", "Install the run system", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		installedSystem, err = installRun(model, gadgetDir, kSnapInfo, "", bopts, installObserver, tm)
	})
	if err != nil {
		return fmt.Errorf("cannot install system: %v", err)
	}

	if trustedInstallObserver != nil {
		// We are required to call ObserveExistingTrustedRecoveryAssets on trusted observers
		if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
			return fmt.Errorf("cannot observe existing trusted recovery assets: %v", err)
		}
	}

	if useEncryption {
		if err := installLogic.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	if err := installLogicPrepareRunSystemData(model, gadgetDir, perfTimings); err != nil {
		return err
	}

	// make it bootable, which should be the final step in the process, as
	// it effectively makes it possible to boot into run mode
	logger.Noticef("make system runnable")
	bootBaseInfo, err := snapstate.BootBaseInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get boot base info: %v", err)
	}
	bootWith := &boot.BootableSet{
		Base:              bootBaseInfo,
		BasePath:          bootBaseInfo.MountFile(),
		Gadget:            gadgetInfo,
		GadgetPath:        gadgetInfo.MountFile(),
		Kernel:            kernelInfo,
		KernelPath:        kernelInfo.MountFile(),
		UnpackedGadgetDir: gadgetDir,

		RecoverySystemLabel: modeEnv.RecoverySystem,
	}
	timings.Run(perfTimings, "boot-make-runnable", "Make target system runnable", func(timings.Measurer) {
		err = bootMakeRunnable(deviceCtx.Model(), bootWith, trustedInstallObserver)
	})
	if err != nil {
		return fmt.Errorf("cannot make system runnable: %v", err)
	}

	return nil
}

// RebootOptions can be attached to restart-system-to-run-mode tasks to control
// their restart behavior.
type RebootOptions struct {
	Op string `json:"op,omitempty"`
}

const (
	RebootHaltOp     = "halt"
	RebootPoweroffOp = "poweroff"
)

func (m *DeviceManager) doRestartSystemToRunMode(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}

	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}
	model := deviceCtx.Model()

	preseeded, err := maybeApplyPreseededData(model, boot.InitramfsUbuntuSeedDir, modeEnv.RecoverySystem, boot.InstallHostWritableDir(model))
	if err != nil {
		logger.Noticef("failed to apply preseed data: %v", err)
		return err
	}
	if preseeded {
		logger.Noticef("successfully preseeded the system")
	} else {
		logger.Noticef("preseed data not present, will do normal seeding")
	}

	// if the model has a gadget snap, and said gadget snap has an install-device hook
	// call systemctl daemon-reload to account for any potential side-effects of that
	// install-device hook
	hasHook, err := m.hasInstallDeviceHook(model)
	if err != nil {
		return err
	}
	if hasHook {
		sd := systemd.New(systemd.SystemMode, progress.Null)
		if err := sd.DaemonReload(); err != nil {
			return err
		}
	}

	// ensure the next boot goes into run mode
	if err := bootEnsureNextBootToRunMode(modeEnv.RecoverySystem); err != nil {
		return err
	}

	var rebootOpts RebootOptions
	err = t.Get("reboot", &rebootOpts)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// write timing information
	if err := writeTimings(st, boot.InstallHostWritableDir(model), modeEnv.Mode); err != nil {
		logger.Noticef("cannot write timings: %v", err)
	}
	// store install-mode log into ubuntu-data partition
	if err := writeLogs(boot.InstallHostWritableDir(model), modeEnv.Mode); err != nil {
		logger.Noticef("cannot write installation log: %v", err)
	}

	// request by default a restart as the last action after a
	// successful install or what install-device requested via
	// snapctl reboot
	rst := restart.RestartSystemNow
	what := "restart"
	switch rebootOpts.Op {
	case RebootHaltOp:
		what = "halt"
		rst = restart.RestartSystemHaltNow
	case RebootPoweroffOp:
		what = "poweroff"
		rst = restart.RestartSystemPoweroffNow
	}
	logger.Noticef("request immediate system %s", what)
	restart.Request(st, rst, nil)

	return nil
}

var seedOpen = seed.Open

func maybeApplyPreseededData(model *asserts.Model, ubuntuSeedDir, sysLabel, writableDir string) (preseeded bool, err error) {
	sysSeed, err := seedOpen(ubuntuSeedDir, sysLabel)
	if err != nil {
		return false, err
	}
	// this function is for UC20+ only so sysSeed ia always PreseedCapable
	preseedSeed := sysSeed.(seed.PreseedCapable)

	if !preseedSeed.HasArtifact("preseed.tgz") {
		return false, nil
	}

	if err := preseedSeed.LoadAssertions(nil, nil); err != nil {
		return false, err
	}
	_, sig := model.Signature()
	_, seedModelSig := preseedSeed.Model().Signature()
	if !bytes.Equal(sig, seedModelSig) {
		return false, fmt.Errorf("system seed %q model does not match model in use", sysLabel)
	}

	if err := applyPreseededData(preseedSeed, writableDir); err != nil {
		return false, err
	}
	return true, nil
}

var applyPreseededData = installLogic.ApplyPreseededData

func (m *DeviceManager) doFactoryResetRunSystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)
	// get gadget dir
	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}
	gadgetInfo, err := snapstate.GadgetInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get gadget info: %v", err)
	}
	gadgetDir := gadgetInfo.MountDir()

	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get kernel info: %v", err)
	}
	kernelDir := kernelInfo.MountDir()

	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}
	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	// bootstrap
	bopts := install.Options{
		Mount: true,
	}
	encryptionType, err := m.checkEncryption(st, deviceCtx, secboot.TPMPartialReprovision)
	if err != nil {
		return err
	}
	bopts.EncryptionType = encryptionType
	useEncryption := (encryptionType != secboot.EncryptionTypeNone)
	hasMarker := device.HasEncryptedMarkerUnder(boot.InstallHostFDESaveDir)
	// TODO verify that the same encryption mechanism is used
	if hasMarker != useEncryption {
		prevStatus := "encrypted"
		if !hasMarker {
			prevStatus = "unencrypted"
		}
		return fmt.Errorf("cannot perform factory reset using different encryption, the original system was %v", prevStatus)
	}

	model := deviceCtx.Model()

	// make sure that gadget is usable for the set up we want to use it in
	validationConstraints := gadget.ValidationConstraints{
		EncryptedData: useEncryption,
	}
	var ginfo *gadget.Info
	timings.Run(perfTimings, "read-info-and-validate", "Read and validate gagdet info", func(timings.Measurer) {
		ginfo, err = gadget.ReadInfoAndValidate(gadgetDir, model, &validationConstraints)
	})
	if err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}
	if err := gadget.ValidateContent(ginfo, gadgetDir, kernelDir); err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}

	var trustedInstallObserver boot.TrustedAssetsInstallObserver
	// get a nice nil interface by default
	var installObserver gadget.ContentObserver
	trustedInstallObserver, err = boot.TrustedAssetsInstallObserverForModel(model, gadgetDir, useEncryption)
	if err != nil && err != boot.ErrObserverNotApplicable {
		return fmt.Errorf("cannot setup asset install observer: %v", err)
	}
	if err == nil {
		installObserver = trustedInstallObserver
		if !useEncryption {
			// there will be no key sealing, so past the
			// installation pass no other methods need to be called
			trustedInstallObserver = nil
		}
	}

	var installedSystem *install.InstalledSystemSideData
	// run the create partition code
	logger.Noticef("create and deploy partitions")
	kSnapInfo := &install.KernelSnapInfo{
		Name:       kernelInfo.SnapName(),
		MountPoint: kernelDir,
		Revision:   kernelInfo.Revision,
		IsCore:     !deviceCtx.Classic(),
	}
	if snapstate.NeedsKernelSetup(deviceCtx) {
		kSnapInfo.NeedsDriversTree = true
	}
	timings.Run(perfTimings, "factory-reset", "Factory reset", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		installedSystem, err = installFactoryReset(model, gadgetDir, kSnapInfo, "", bopts, installObserver, tm)
	})
	if err != nil {
		return fmt.Errorf("cannot perform factory reset: %v", err)
	}
	logger.Noticef("devs: %+v", installedSystem.DeviceForRole)

	if trustedInstallObserver != nil {
		// We are required to call ObserveExistingTrustedRecoveryAssets on trusted observers
		if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
			return fmt.Errorf("cannot observe existing trusted recovery assets: %v", err)
		}
	}

	if useEncryption {
		// at this point we removed boot and data. sealed fallback key
		// for ubuntu-data is becoming useless
		err := os.Remove(device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot cleanup obsolete key file: %v", err)
		}

		// it is possible that we reached this place again where a
		// previously running factory reset was interrupted by a reboot
		err = os.Remove(device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot cleanup obsolete key file: %v", err)
		}

		// it is ok if the recovery key file on disk does not exist;
		// ubuntu-save was opened during boot, so the removal operation
		// can be authorized with a key from the keyring
		err = secbootRemoveRecoveryKeys(map[secboot.RecoveryKeyDevice]string{
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: device.RecoveryKeyUnder(boot.InstallHostFDEDataDir(model)),
		})
		if err != nil {
			return fmt.Errorf("cannot remove recovery key: %v", err)
		}

		// new encryption key for save
		saveEncryptionKey, err := keys.NewEncryptionKey()
		if err != nil {
			return fmt.Errorf("cannot create encryption key: %v", err)
		}

		saveNode := installedSystem.DeviceForRole[gadget.SystemSave]
		if saveNode == "" {
			return fmt.Errorf("internal error: no system-save device")
		}

		if err := secbootStageEncryptionKeyChange(saveNode, saveEncryptionKey); err != nil {
			return fmt.Errorf("cannot change encryption keys: %v", err)
		}
		// keep track of the new ubuntu-save encryption key
		installedSystem.KeyForRole[gadget.SystemSave] = saveEncryptionKey

		if err := installLogic.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	if err := installLogicPrepareRunSystemData(model, gadgetDir, perfTimings); err != nil {
		return err
	}

	if err := restoreDeviceFromSave(model); err != nil {
		return fmt.Errorf("cannot restore data from save: %v", err)
	}

	// make it bootable
	logger.Noticef("make system runnable")
	bootBaseInfo, err := snapstate.BootBaseInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get boot base info: %v", err)
	}
	bootWith := &boot.BootableSet{
		Base:              bootBaseInfo,
		BasePath:          bootBaseInfo.MountFile(),
		Gadget:            gadgetInfo,
		GadgetPath:        gadgetInfo.MountFile(),
		Kernel:            kernelInfo,
		KernelPath:        kernelInfo.MountFile(),
		UnpackedGadgetDir: gadgetDir,

		RecoverySystemLabel: modeEnv.RecoverySystem,
	}
	timings.Run(perfTimings, "boot-make-runnable", "Make target system runnable", func(timings.Measurer) {
		err = bootMakeRunnableAfterDataReset(deviceCtx.Model(), bootWith, trustedInstallObserver)
	})
	if err != nil {
		return fmt.Errorf("cannot make system runnable: %v", err)
	}

	// leave a marker that factory reset was performed
	factoryResetMarker := filepath.Join(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir(model)), "factory-reset")
	if err := writeFactoryResetMarker(factoryResetMarker, useEncryption); err != nil {
		return fmt.Errorf("cannot write the marker file: %v", err)
	}
	return nil
}

func restoreDeviceFromSave(model *asserts.Model) error {
	// we could also look at factory-reset-bootstrap.json left by
	// snap-bootstrap, but the mount was already verified during boot
	mounted, err := osutil.IsMounted(boot.InitramfsUbuntuSaveDir)
	if err != nil {
		return fmt.Errorf("cannot determine ubuntu-save mount state: %v", err)
	}
	if !mounted {
		logger.Noticef("not restoring from save, ubuntu-save not mounted")
		return nil
	}
	// TODO anything else we want to restore?
	return restoreDeviceSerialFromSave(model)
}

func restoreDeviceSerialFromSave(model *asserts.Model) error {
	fromDevice := filepath.Join(boot.InstallHostDeviceSaveDir)
	logger.Debugf("looking for serial assertion and device key under %v", fromDevice)
	fromDB, err := sysdb.OpenAt(fromDevice)
	if err != nil {
		return err
	}
	// key pair manager always uses ubuntu-save whenever it's available
	kp, err := asserts.OpenFSKeypairManager(fromDevice)
	if err != nil {
		return err
	}
	// there should be a serial assertion for the current model
	serials, err := fromDB.FindMany(asserts.SerialType, map[string]string{
		"brand-id": model.BrandID(),
		"model":    model.Model(),
	})
	if (err != nil && errors.Is(err, &asserts.NotFoundError{})) || len(serials) == 0 {
		// there is no serial assertion in the old system that matches
		// our model, it is still possible that the old system could
		// have generated device keys and sent out a serial request, but
		// for simplicity we ignore this scenario and a new set of keys
		// will be generated after booting into the run system
		logger.Debugf("no serial assertion for %v/%v", model.BrandID(), model.Model())
		return nil
	}
	if err != nil {
		return err
	}
	logger.Noticef("found %v serial assertions for %v/%v", len(serials), model.BrandID(), model.Model())

	var serialAs *asserts.Serial
	for _, serial := range serials {
		maybeCurrentSerialAs := serial.(*asserts.Serial)
		// serial assertion is signed with the device key, its ID is in the
		// header
		deviceKeyID := maybeCurrentSerialAs.DeviceKey().ID()
		logger.Debugf("serial assertion device key ID: %v", deviceKeyID)

		// there can be multiple serial assertions, as the device could
		// have exercised the registration a number of times, but each
		// time it unregisters, the old key is removed and a new one is
		// generated
		_, err = kp.Get(deviceKeyID)
		if err != nil {
			if asserts.IsKeyNotFound(err) {
				logger.Debugf("no key with ID %v", deviceKeyID)
				continue
			}
			return fmt.Errorf("cannot obtain device key: %v", err)
		} else {
			serialAs = maybeCurrentSerialAs
			break
		}
	}

	if serialAs == nil {
		// no serial assertion that matches the model, brand and is
		// signed with a device key that is present in the filesystem
		logger.Debugf("no valid serial assertions")
		return nil
	}

	logger.Debugf("found a serial assertion for %v/%v, with serial %v",
		model.BrandID(), model.Model(), serialAs.Serial())

	toDB, err := sysdb.OpenAt(filepath.Join(boot.InstallHostWritableDir(model), "var/lib/snapd/assertions"))
	if err != nil {
		return err
	}

	logger.Debugf("importing serial and model assertions")
	b := asserts.NewBatch(nil)
	err = b.Fetch(toDB,
		func(ref *asserts.Ref) (asserts.Assertion, error) { return ref.Resolve(fromDB.Find) },
		func(f asserts.Fetcher) error {
			if err := f.Save(model); err != nil {
				return err
			}
			return f.Save(serialAs)
		})
	if err != nil {
		return fmt.Errorf("cannot fetch assertions: %v", err)
	}
	if err := b.CommitTo(toDB, nil); err != nil {
		return fmt.Errorf("cannot commit assertions: %v", err)
	}
	return nil
}

type factoryResetMarker struct {
	FallbackSaveKeyHash string `json:"fallback-save-key-sha3-384,omitempty"`
}

func fileDigest(p string) (string, error) {
	digest, _, err := osutil.FileDigest(p, crypto.SHA3_384)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest), nil
}

func writeFactoryResetMarker(marker string, hasEncryption bool) error {
	keyDigest := ""
	if hasEncryption {
		d, err := fileDigest(device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir))
		if err != nil {
			return err
		}
		keyDigest = d
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(factoryResetMarker{
		FallbackSaveKeyHash: keyDigest,
	})
	if err != nil {
		return err
	}

	if hasEncryption {
		logger.Noticef("writing factory-reset marker at %v with key digest %q", marker, keyDigest)
	} else {
		logger.Noticef("writing factory-reset marker at %v", marker)
	}
	return osutil.AtomicWriteFile(marker, buf.Bytes(), 0644, 0)
}

func verifyFactoryResetMarkerInRun(marker string, hasEncryption bool) error {
	f, err := os.Open(marker)
	if err != nil {
		return err
	}
	defer f.Close()
	var frm factoryResetMarker
	if err := json.NewDecoder(f).Decode(&frm); err != nil {
		return err
	}
	if hasEncryption {
		saveFallbackKeyFactory := device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
		d, err := fileDigest(saveFallbackKeyFactory)
		if err != nil {
			// possible that there was unexpected reboot
			// before, after the key was moved, but before
			// the marker was removed, in which case the
			// actual fallback key should have the right
			// digest
			if !os.IsNotExist(err) {
				// unless it's a different error
				return err
			}
			saveFallbackKeyFactory := device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
			d, err = fileDigest(saveFallbackKeyFactory)
			if err != nil {
				return err
			}
		}
		if d != frm.FallbackSaveKeyHash {
			return fmt.Errorf("fallback sealed key digest mismatch, got %v expected %v", d, frm.FallbackSaveKeyHash)
		}
	} else {
		if frm.FallbackSaveKeyHash != "" {
			return fmt.Errorf("unexpected non-empty fallback key digest")
		}
	}
	return nil
}

func rotateEncryptionKeys() error {
	kd, err := os.ReadFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"))
	if err != nil {
		return fmt.Errorf("cannot open encryption key file: %v", err)
	}
	// does the right thing if the key has already been transitioned
	if err := secbootTransitionEncryptionKeyChange(boot.InitramfsUbuntuSaveDir, keys.EncryptionKey(kd)); err != nil {
		return fmt.Errorf("cannot transition the encryption key: %v", err)
	}
	return nil
}

type encryptionSetupDataKey struct {
	systemLabel string
}

func mountSeedSnap(seedSn *seed.Snap) (mountpoint string, unmount func() error, err error) {
	mountpoint = filepath.Join(dirs.SnapRunDir, "snap-content", string(seedSn.EssentialType))
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return "", nil, err
	}

	// temporarily mount the filesystem
	logger.Debugf("mounting %q in %q", seedSn.Path, mountpoint)
	sd := systemd.New(systemd.SystemMode, progress.Null)
	if err := sd.Mount(seedSn.Path, mountpoint); err != nil {
		return "", nil, fmt.Errorf("cannot mount %q at %q: %v", seedSn.Path, mountpoint, err)
	}
	return mountpoint,
		func() error {
			logger.Debugf("unmounting %q", mountpoint)
			return sd.Umount(mountpoint)
		},
		nil
}

func (m *DeviceManager) loadAndMountSystemLabelSnaps(systemLabel string) (*systemAndEssentialSnaps, map[snap.Type]string, func(), error) {

	essentialTypes := []snap.Type{snap.TypeKernel, snap.TypeBase, snap.TypeGadget}
	systemAndSnaps, err := m.loadSystemAndEssentialSnaps(systemLabel, essentialTypes)
	if err != nil {
		return nil, nil, nil, err
	}
	// Unset revision here actually means that the snap is local.
	// Assign then a local revision as seeding/installing the snap would do.
	for _, snInfo := range systemAndSnaps.InfosByType {
		if snInfo.Revision.Unset() {
			snInfo.Revision = snap.R(-1)
		}
	}

	// Mount gadget and kernel
	var unmountFuncs []func() error
	mntPtForType := make(map[snap.Type]string)
	unmount := func() {
		for _, unmountF := range unmountFuncs {
			if errUnmount := unmountF(); errUnmount != nil {
				logger.Noticef("error unmounting: %v", errUnmount)
			}
		}
	}

	seedSnaps := systemAndSnaps.SeedSnapsByType

	for _, seedSn := range []*seed.Snap{seedSnaps[snap.TypeGadget], seedSnaps[snap.TypeKernel]} {
		mntPt, unmountSnap, err := mountSeedSnap(seedSn)
		if err != nil {
			unmount()
			return nil, nil, nil, err
		}
		unmountFuncs = append(unmountFuncs, unmountSnap)
		mntPtForType[seedSn.EssentialType] = mntPt
	}

	return systemAndSnaps, mntPtForType, unmount, nil
}

// doInstallFinish performs the finish step of the install. It will
// - install missing volumes structure content
// - copy seed (only for UC)
// - install gadget assets
// - install kernel.efi
// - make system bootable (including writing modeenv)
func (m *DeviceManager) doInstallFinish(t *state.Task, _ *tomb.Tomb) error {
	var err error
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	var systemLabel string
	if err := t.Get("system-label", &systemLabel); err != nil {
		return err
	}
	var onVolumes map[string]*gadget.Volume
	if err := t.Get("on-volumes", &onVolumes); err != nil {
		return err
	}

	var encryptSetupData *install.EncryptionSetupData
	cached := st.Cached(encryptionSetupDataKey{systemLabel})
	if cached != nil {
		var ok bool
		// TODO check that encryptSetupData is not out of sync with the onVolumes we get
		encryptSetupData, ok = cached.(*install.EncryptionSetupData)
		if !ok {
			return fmt.Errorf("internal error: wrong data type under encryptionSetupDataKey")
		}
	}

	st.Unlock()
	systemAndSnaps, mntPtForType, unmount, err := m.loadAndMountSystemLabelSnaps(systemLabel)
	st.Lock()
	if err != nil {
		return err
	}
	defer unmount()

	// Check if encryption is mandatory
	if systemAndSnaps.Model.StorageSafety() == asserts.StorageSafetyEncrypted && encryptSetupData == nil {
		return fmt.Errorf("storage encryption required by model but has not been set up")
	}
	useEncryption := encryptSetupData != nil

	logger.Debugf("starting install-finish for %q (using encryption: %t) on %v", systemLabel, useEncryption, onVolumes)

	// TODO we probably want to pass a different location for the assets cache
	installObserver, trustedInstallObserver, err := installLogic.BuildInstallObserver(systemAndSnaps.Model, mntPtForType[snap.TypeGadget], useEncryption)
	if err != nil {
		return err
	}

	gi, err := gadget.ReadInfoAndValidate(mntPtForType[snap.TypeGadget], systemAndSnaps.Model, nil)
	if err != nil {
		return err
	}

	// Import new information from the installer to the gadget data,
	// including the target devices and information marked as partial in
	// the gadget, so the gadget is not partially defined anymore if it
	// was.
	// TODO validation of onVolumes versus gadget.yaml, needs to happen here.
	mergedVols, err := gadget.ApplyInstallerVolumesToGadget(onVolumes, gi.Volumes)
	if err != nil {
		return err
	}

	// Match gadget against the disk, so we make sure that the information
	// reported by the installer is correct and that all partitions have
	// been created.
	volCompatOpts := &gadget.VolumeCompatibilityOptions{
		// at this point all partitions should be created
		AssumeCreatablePartitionsCreated: true,
	}
	if useEncryption {
		volCompatOpts.ExpectedStructureEncryption = map[string]gadget.StructureEncryptionParameters{
			"ubuntu-data": {Method: gadget.EncryptionLUKS},
			"ubuntu-save": {Method: gadget.EncryptionLUKS},
		}
	}
	volToGadgetToDiskStruct, err := installMatchDisksToGadgetVolumes(mergedVols, volCompatOpts)
	if err != nil {
		return err
	}

	encType := secboot.EncryptionTypeNone
	// TODO:ICE: support secboot.EncryptionTypeLUKSWithICE in the API
	if useEncryption {
		encType = secboot.EncryptionTypeLUKS
	}
	kernMntPoint := mntPtForType[snap.TypeKernel]
	allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(mergedVols,
		mntPtForType[snap.TypeGadget], kernMntPoint,
		encType, volToGadgetToDiskStruct)
	if err != nil {
		return fmt.Errorf("on finish install: cannot layout volumes: %v", err)
	}

	snapInfos := systemAndSnaps.InfosByType
	snapSeeds := systemAndSnaps.SeedSnapsByType
	kernInfo := snapInfos[snap.TypeKernel]
	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}

	logger.Debugf("writing content to partitions")
	kSnapInfo := &install.KernelSnapInfo{
		Name:       kernInfo.SnapName(),
		Revision:   kernInfo.Revision,
		MountPoint: kernMntPoint,
		IsCore:     !deviceCtx.Classic(),
	}
	if snapstate.NeedsKernelSetup(deviceCtx) {
		kSnapInfo.NeedsDriversTree = true
	}
	timings.Run(perfTimings, "install-content", "Writing content to partitions", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		_, err = installWriteContent(mergedVols, allLaidOutVols, encryptSetupData, kSnapInfo, installObserver, perfTimings)
	})
	if err != nil {
		return fmt.Errorf("cannot write content: %v", err)
	}

	// Mount the partitions and find the system-seed{,-null} partition
	seedMntDir, unmountParts, err := installMountVolumes(mergedVols, encryptSetupData)
	if err != nil {
		return fmt.Errorf("cannot mount partitions for installation: %v", err)
	}
	defer unmountParts()

	if !systemAndSnaps.Model.Classic() {
		copier, ok := systemAndSnaps.Seed.(seed.Copier)
		if !ok {
			return fmt.Errorf("internal error: seed does not support copying: %s", systemAndSnaps.Label)
		}

		logger.Debugf("copying label %q to seed partition", systemAndSnaps.Label)
		if err := copier.Copy(seedMntDir, systemAndSnaps.Label, perfTimings); err != nil {
			return fmt.Errorf("cannot copy seed: %w", err)
		}
	}

	if err := installSaveStorageTraits(systemAndSnaps.Model, mergedVols, encryptSetupData); err != nil {
		return err
	}

	if trustedInstallObserver != nil {
		// We are required to call ObserveExistingTrustedRecoveryAssets on trusted observers
		if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
			return fmt.Errorf("cannot observe existing trusted recovery assets: %v", err)
		}
	}

	if useEncryption {
		if trustedInstallObserver != nil {
			if err := installLogic.PrepareEncryptedSystemData(systemAndSnaps.Model, install.KeysForRole(encryptSetupData), trustedInstallObserver); err != nil {
				return err
			}
		}
	}

	bootWith := &boot.BootableSet{
		Base:              snapInfos[snap.TypeBase],
		BasePath:          snapSeeds[snap.TypeBase].Path,
		Kernel:            snapInfos[snap.TypeKernel],
		KernelPath:        snapSeeds[snap.TypeKernel].Path,
		Gadget:            snapInfos[snap.TypeGadget],
		GadgetPath:        snapSeeds[snap.TypeGadget].Path,
		UnpackedGadgetDir: mntPtForType[snap.TypeGadget],

		RecoverySystemLabel: systemLabel,
	}

	// installs in system-seed{,-null} partition: grub.cfg, grubenv
	logger.Debugf("making the system-seed{,-null} partition bootable, mount dir is %q", seedMntDir)
	opts := &bootloader.Options{
		PrepareImageTime: false,
		// We need the same configuration that a recovery partition,
		// as we will chainload to grub in the boot partition.
		Role: bootloader.RoleRecovery,
	}
	if err := bootMakeBootablePartition(seedMntDir, opts, bootWith, boot.ModeRun, nil); err != nil {
		return err
	}

	// writes the model etc
	if err := installLogicPrepareRunSystemData(systemAndSnaps.Model, bootWith.UnpackedGadgetDir, perfTimings); err != nil {
		return err
	}

	logger.Debugf("making the installed system runnable for system label %s", systemLabel)
	if err := bootMakeRunnableStandalone(systemAndSnaps.Model, bootWith, trustedInstallObserver, st.Unlocker()); err != nil {
		return err
	}

	return nil
}

func (m *DeviceManager) doInstallSetupStorageEncryption(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	var systemLabel string
	if err := t.Get("system-label", &systemLabel); err != nil {
		return err
	}
	var onVolumes map[string]*gadget.Volume
	if err := t.Get("on-volumes", &onVolumes); err != nil {
		return err
	}
	logger.Debugf("install-setup-storage-encryption for %q on %v", systemLabel, onVolumes)

	st.Unlock()
	systemAndSeeds, mntPtForType, unmount, err := m.loadAndMountSystemLabelSnaps(systemLabel)
	st.Lock()
	if err != nil {
		return err
	}
	defer unmount()

	// Gadget information
	snapf, err := snapfile.Open(systemAndSeeds.SeedSnapsByType[snap.TypeGadget].Path)
	if err != nil {
		return fmt.Errorf("cannot open gadget snap: %v", err)
	}
	gadgetInfo, err := gadget.ReadInfoFromSnapFileNoValidate(snapf, systemAndSeeds.Model)
	if err != nil {
		return fmt.Errorf("reading gadget information: %v", err)
	}

	encryptInfo, err := m.encryptionSupportInfo(systemAndSeeds.Model, secboot.TPMProvisionFull, systemAndSeeds.InfosByType[snap.TypeKernel], gadgetInfo)
	if err != nil {
		return err
	}
	if !encryptInfo.Available {
		var whyStr string
		if encryptInfo.UnavailableErr != nil {
			whyStr = encryptInfo.UnavailableErr.Error()
		} else {
			whyStr = encryptInfo.UnavailableWarning
		}
		return fmt.Errorf("encryption unavailable on this device: %v", whyStr)
	}

	// TODO:ICE: support secboot.EncryptionTypeLUKSWithICE in the API
	encType := secboot.EncryptionTypeLUKS
	encryptionSetupData, err := installEncryptPartitions(onVolumes, encType, systemAndSeeds.Model, mntPtForType[snap.TypeGadget], mntPtForType[snap.TypeKernel], perfTimings)
	if err != nil {
		return err
	}

	// Store created devices in the change so they can be accessed from the installer
	apiData := map[string]interface{}{
		"encrypted-devices": encryptionSetupData.EncryptedDevices(),
	}
	chg := t.Change()
	chg.Set("api-data", apiData)

	st.Cache(encryptionSetupDataKey{systemLabel}, encryptionSetupData)

	return nil
}
