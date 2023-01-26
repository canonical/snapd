// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2022 Canonical Ltd
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
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	_ "golang.org/x/crypto/sha3"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	installHandler "github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/sysconfig"
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
	secbootStageEncryptionKeyChange      = secboot.StageEncryptionKeyChange
	secbootTransitionEncryptionKeyChange = secboot.TransitionEncryptionKeyChange

	sysconfigConfigureTargetSystem = sysconfig.ConfigureTargetSystem
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

	modeEnv, err := installHandler.MaybeReadModeenv()
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

	installObserver, trustedInstallObserver, err := installHandler.BuildInstallObserver(model, gadgetDir, useEncryption)
	if err != nil {
		return err
	}

	var installedSystem *install.InstalledSystemSideData
	// run the create partition code
	logger.Noticef("create and deploy partitions")
	timings.Run(perfTimings, "install-run", "Install the run system", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		installedSystem, err = installRun(model, gadgetDir, kernelDir, "", bopts, installObserver, tm)
	})
	if err != nil {
		return fmt.Errorf("cannot install system: %v", err)
	}

	if trustedInstallObserver != nil {
		if err := installHandler.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	if err := installHandler.PrepareRunSystemData(model, gadgetDir, perfTimings); err != nil {
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

	modeEnv, err := installHandler.MaybeReadModeenv()
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

	preseeded, err := maybeApplyPreseededData(st, model, boot.InitramfsUbuntuSeedDir, modeEnv.RecoverySystem, boot.InstallHostWritableDir(model))
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

var maybeApplyPreseededData = func(st *state.State, model *asserts.Model, ubuntuSeedDir, sysLabel, writableDir string) (preseeded bool, err error) {
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	if !osutil.FileExists(preseedArtifact) {
		return false, nil
	}
	// main seed assertions are loaded in the assertion db of install mode; add preseed assertion from
	// systems/<label>/preseed file on top of it via a temporary db.
	preseedAs, err := installHandler.ReadPreseedAssertion(assertstate.TemporaryDB(st), model, ubuntuSeedDir, sysLabel)
	if err != nil {
		return false, err
	}
	if err = installHandler.MaybeApplyPreseededData(model, preseedAs, preseedArtifact, ubuntuSeedDir, sysLabel, writableDir); err != nil {
		return false, err
	} else {
		return true, nil
	}
}

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

	modeEnv, err := installHandler.MaybeReadModeenv()
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

	var trustedInstallObserver *boot.TrustedAssetsInstallObserver
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
	timings.Run(perfTimings, "factory-reset", "Factory reset", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		installedSystem, err = installFactoryReset(model, gadgetDir, kernelDir, "", bopts, installObserver, tm)
	})
	if err != nil {
		return fmt.Errorf("cannot perform factory reset: %v", err)
	}
	logger.Noticef("devs: %+v", installedSystem.DeviceForRole)

	if trustedInstallObserver != nil {
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

		if err := installHandler.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	if err := installHandler.PrepareRunSystemData(model, gadgetDir, perfTimings); err != nil {
		return err
	}

	if err := installHandler.RestoreDeviceFromSave(model); err != nil {
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
	if err := installHandler.WriteFactoryResetMarker(factoryResetMarker, useEncryption); err != nil {
		return fmt.Errorf("cannot write the marker file: %v", err)
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

func (m *DeviceManager) loadAndMountSystemLabelSnaps(systemLabel string) (
	*installHandler.System, map[snap.Type]*snap.Info, map[snap.Type]*seed.Snap, map[snap.Type]string, func(), error) {

	essentialTypes := []snap.Type{snap.TypeKernel, snap.TypeBase, snap.TypeGadget}
	sys, snapInfos, snapSeeds, err := m.loadSystemAndEssentialSnaps(systemLabel, essentialTypes)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	// Unset revision here actually means that the snap is local.
	// Assign then a local revision as seeding/installing the snap would do.
	for _, snInfo := range snapInfos {
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
	for _, seedSn := range []*seed.Snap{snapSeeds[snap.TypeGadget], snapSeeds[snap.TypeKernel]} {
		mntPt, unmountSnap, err := mountSeedSnap(seedSn)
		if err != nil {
			unmount()
			return nil, nil, nil, nil, nil, err
		}
		unmountFuncs = append(unmountFuncs, unmountSnap)
		mntPtForType[seedSn.EssentialType] = mntPt
	}

	return sys, snapInfos, snapSeeds, mntPtForType, unmount, nil
}

// doInstallFinish performs the finish step of the install. It will
// - install missing volumes structure content
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
	sys, snapInfos, snapSeeds, mntPtForType, unmount, err := m.loadAndMountSystemLabelSnaps(systemLabel)
	st.Lock()
	if err != nil {
		return err
	}
	defer unmount()

	// TODO validation of onVolumes versus gadget.yaml

	// Check if encryption is mandatory
	if sys.Model.StorageSafety() == asserts.StorageSafetyEncrypted && encryptSetupData == nil {
		return fmt.Errorf("storage encryption required by model but has not been set up")
	}
	useEncryption := encryptSetupData != nil

	logger.Debugf("starting install-finish for %q (using encryption: %t) on %v", systemLabel, useEncryption, onVolumes)

	// TODO we probably want to pass a different location for the assets cache
	installObserver, trustedInstallObserver, err := installHandler.BuildInstallObserver(sys.Model, mntPtForType[snap.TypeGadget], useEncryption)
	if err != nil {
		return err
	}

	encType := secboot.EncryptionTypeNone
	// TODO:ICE: support secboot.EncryptionTypeLUKSWithICE in the API
	if useEncryption {
		encType = secboot.EncryptionTypeLUKS
	}
	// TODO for partial gadgets we should also use the data from onVolumes instead of
	// using only what comes from gadget.yaml.
	_, allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(mntPtForType[snap.TypeGadget], mntPtForType[snap.TypeKernel], sys.Model, encType)
	if err != nil {
		return fmt.Errorf("on finish install: cannot layout volumes: %v", err)
	}

	logger.Debugf("writing content to partitions")
	timings.Run(perfTimings, "install-content", "Writing content to partitions", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		_, err = installWriteContent(onVolumes, allLaidOutVols, encryptSetupData, installObserver, perfTimings)
	})
	if err != nil {
		return fmt.Errorf("cannot write content: %v", err)
	}

	// Mount the partitions and find the system-seed{,-null} partition
	seedMntDir, unmountParts, err := installMountVolumes(onVolumes, encryptSetupData)
	if err != nil {
		return fmt.Errorf("cannot mount partitions for installation: %v", err)
	}
	defer unmountParts()

	if err := installSaveStorageTraits(sys.Model, allLaidOutVols, encryptSetupData); err != nil {
		return err
	}

	if useEncryption {
		if trustedInstallObserver != nil {
			if err := installHandler.PrepareEncryptedSystemData(sys.Model, install.KeysForRole(encryptSetupData), trustedInstallObserver); err != nil {
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

	// writes the model
	if err := installHandler.PrepareRunSystemData(sys.Model, bootWith.UnpackedGadgetDir, perfTimings); err != nil {
		return err
	}

	logger.Debugf("making the installed system runnable for system label %s", systemLabel)
	if err := bootMakeRunnableStandalone(sys.Model, bootWith, trustedInstallObserver); err != nil {
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
	sys, snapInfos, snapSeeds, mntPtForType, unmount, err := m.loadAndMountSystemLabelSnaps(systemLabel)
	st.Lock()
	if err != nil {
		return err
	}
	defer unmount()

	// Gadget information
	snapf, err := snapfile.Open(snapSeeds[snap.TypeGadget].Path)
	if err != nil {
		return fmt.Errorf("cannot open gadget snap: %v", err)
	}
	gadgetInfo, err := gadget.ReadInfoFromSnapFileNoValidate(snapf, sys.Model)
	if err != nil {
		return fmt.Errorf("reading gadget information: %v", err)
	}

	encryptInfo, err := installHandler.GetEncryptionSupportInfo(sys.Model, secboot.TPMProvisionFull, snapInfos[snap.TypeKernel], gadgetInfo, m.runFDESetupHook)
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
	encryptionSetupData, err := installEncryptPartitions(onVolumes, encType, sys.Model, mntPtForType[snap.TypeGadget], mntPtForType[snap.TypeKernel], perfTimings)
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
