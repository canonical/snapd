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

package devicestate

import (
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/timings"
)

var (
	bootMakeRunnable            = boot.MakeRunnableSystem
	bootEnsureNextBootToRunMode = boot.EnsureNextBootToRunMode
	installRun                  = install.Run
	installFactoryReset         = func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, fmt.Errorf("not implemented")
	}

	sysconfigConfigureTargetSystem = sysconfig.ConfigureTargetSystem
)

func setSysconfigCloudOptions(opts *sysconfig.Options, gadgetDir string, model *asserts.Model) {
	ubuntuSeedCloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")

	grade := model.Grade()

	// we always set the cloud-init src directory if it exists, it is
	// automatically ignored by sysconfig in the case it shouldn't be used
	if osutil.IsDirectory(ubuntuSeedCloudCfg) {
		opts.CloudInitSrcDir = ubuntuSeedCloudCfg
	}

	switch {
	// if the gadget has a cloud.conf file, always use that regardless of grade
	case sysconfig.HasGadgetCloudConf(gadgetDir):
		opts.AllowCloudInit = true

	// next thing is if are in secured grade and didn't have gadget config, we
	// disable cloud-init always, clouds should have their own config via
	// gadgets for grade secured
	case grade == asserts.ModelSecured:
		opts.AllowCloudInit = false

	// all other cases we allow cloud-init to run, either through config that is
	// available at runtime via a CI-DATA USB drive, or via config on
	// ubuntu-seed if that is allowed by the model grade, etc.
	default:
		opts.AllowCloudInit = true
	}
}

func writeModel(model *asserts.Model, where string) error {
	f, err := os.OpenFile(where, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return asserts.NewEncoder(f).Encode(model)
}

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

func writeTimesyncdClock(srcRootDir, dstRootDir string) error {
	// keep track of the time
	const timesyncClockInRoot = "/var/lib/systemd/timesync/clock"
	clockSrc := filepath.Join(srcRootDir, timesyncClockInRoot)
	clockDst := filepath.Join(dstRootDir, timesyncClockInRoot)
	if err := os.MkdirAll(filepath.Dir(clockDst), 0755); err != nil {
		return fmt.Errorf("cannot store the clock: %v", err)
	}
	if !osutil.FileExists(clockSrc) {
		logger.Noticef("timesyncd clock timestamp %v does not exist", clockSrc)
		return nil
	}
	// clock file is owned by a specific user/group, thus preserve
	// attributes of the source
	if err := osutil.CopyFile(clockSrc, clockDst, osutil.CopyFlagPreserveAll); err != nil {
		return fmt.Errorf("cannot copy clock: %v", err)
	}
	// the file is empty however, its modification timestamp is used to set
	// up the current time
	if err := os.Chtimes(clockDst, timeNow(), timeNow()); err != nil {
		return fmt.Errorf("cannot update clock timestamp: %v", err)
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
	encryptionType, err := m.checkEncryption(st, deviceCtx)
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
	timings.Run(perfTimings, "install-run", "Install the run system", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		installedSystem, err = installRun(model, gadgetDir, kernelDir, "", bopts, installObserver, tm)
	})
	if err != nil {
		return fmt.Errorf("cannot install system: %v", err)
	}

	if trustedInstallObserver != nil {
		// validity check
		if installedSystem.KeysForRoles == nil || installedSystem.KeysForRoles[gadget.SystemData] == nil || installedSystem.KeysForRoles[gadget.SystemSave] == nil {
			return fmt.Errorf("internal error: system encryption keys are unset")
		}
		dataKeySet := installedSystem.KeysForRoles[gadget.SystemData]
		saveKeySet := installedSystem.KeysForRoles[gadget.SystemSave]

		// make note of the encryption keys
		trustedInstallObserver.ChosenEncryptionKeys(dataKeySet.Key, saveKeySet.Key)

		// keep track of recovery assets
		if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
			return fmt.Errorf("cannot observe existing trusted recovery assets: err")
		}
		if err := saveKeys(installedSystem.KeysForRoles); err != nil {
			return err
		}
		// write markers containing a secret to pair data and save
		if err := writeMarkers(); err != nil {
			return err
		}
	}

	// keep track of the model we installed
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}
	err = writeModel(model, filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}

	// preserve systemd-timesyncd clock timestamp, so that RTC-less devices
	// can start with a more recent time on the next boot
	if err := writeTimesyncdClock(dirs.GlobalRootDir, boot.InstallHostWritableDir); err != nil {
		return fmt.Errorf("cannot seed timesyncd clock: %v", err)
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir, GadgetDir: gadgetDir}
	// configure cloud init
	setSysconfigCloudOptions(opts, gadgetDir, model)
	timings.Run(perfTimings, "sysconfig-configure-target-system", "Configure target system", func(timings.Measurer) {
		err = sysconfigConfigureTargetSystem(model, opts)
	})
	if err != nil {
		return err
	}

	// TODO: FIXME: this should go away after we have time to design a proper
	//              solution
	// TODO: only run on specific models?

	// on some specific devices, we need to create these directories in
	// _writable_defaults in order to allow the install-device hook to install
	// some files there, this eventually will go away when we introduce a proper
	// mechanism not using system-files to install files onto the root
	// filesystem from the install-device hook
	if err := fixupWritableDefaultDirs(boot.InstallHostWritableDir); err != nil {
		return err
	}

	// make it bootable, which should be the final step in the process, as
	// it effectively makes it possible to boot into run mode
	logger.Noticef("make system runnable")
	bootBaseInfo, err := snapstate.BootBaseInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get boot base info: %v", err)
	}
	recoverySystemDir := filepath.Join("/systems", modeEnv.RecoverySystem)
	bootWith := &boot.BootableSet{
		Base:              bootBaseInfo,
		BasePath:          bootBaseInfo.MountFile(),
		Kernel:            kernelInfo,
		KernelPath:        kernelInfo.MountFile(),
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: gadgetDir,
	}
	timings.Run(perfTimings, "boot-make-runnable", "Make target system runnable", func(timings.Measurer) {
		err = bootMakeRunnable(deviceCtx.Model(), bootWith, trustedInstallObserver)
	})
	if err != nil {
		return fmt.Errorf("cannot make system runnable: %v", err)
	}
	return nil
}

func fixupWritableDefaultDirs(systemDataDir string) error {
	// the _writable_default directory is used to put files in place on
	// ubuntu-data from install mode, so we abuse it here for a specific device
	// to let that device install files with system-files and the install-device
	// hook

	// eventually this will be a proper, supported, designed mechanism instead
	// of just this hack, but this hack is just creating the directories, since
	// the system-files interface only allows creating the file, not creating
	// the directories leading up to that file, and since the file is deeply
	// nested we would effectively have to give all permission to the device
	// to create any file on ubuntu-data which we don't want to do, so we keep
	// this restriction to let the device create one specific file, and then
	// we behind the scenes just create the directories for the device

	for _, subDirToCreate := range []string{"/etc/udev/rules.d", "/etc/modprobe.d", "/etc/modules-load.d/"} {
		dirToCreate := sysconfig.WritableDefaultsDir(systemDataDir, subDirToCreate)

		if err := os.MkdirAll(dirToCreate, 0755); err != nil {
			return err
		}
	}

	return nil
}

// writeMarkers writes markers containing the same secret to pair data and save.
func writeMarkers() error {
	// ensure directory for markers exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(boot.InstallHostFDESaveDir, 0755); err != nil {
		return err
	}

	// generate a secret random marker
	markerSecret, err := randutil.CryptoTokenBytes(32)
	if err != nil {
		return fmt.Errorf("cannot create ubuntu-data/save marker secret: %v", err)
	}

	dataMarker := filepath.Join(boot.InstallHostFDEDataDir, "marker")
	if err := osutil.AtomicWriteFile(dataMarker, markerSecret, 0600, 0); err != nil {
		return err
	}

	saveMarker := filepath.Join(boot.InstallHostFDESaveDir, "marker")
	if err := osutil.AtomicWriteFile(saveMarker, markerSecret, 0600, 0); err != nil {
		return err
	}

	return nil
}

func saveKeys(keysForRoles map[string]*install.EncryptionKeySet) error {
	dataKeySet := keysForRoles[gadget.SystemData]

	// ensure directory for keys exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir, 0755); err != nil {
		return err
	}

	// Write the recovery key
	recoveryKeyFile := filepath.Join(boot.InstallHostFDEDataDir, "recovery.key")
	if err := dataKeySet.RecoveryKey.Save(recoveryKeyFile); err != nil {
		return fmt.Errorf("cannot store recovery key: %v", err)
	}

	saveKeySet := keysForRoles[gadget.SystemSave]
	if saveKeySet == nil {
		// no system-save support
		return nil
	}

	saveKey := filepath.Join(boot.InstallHostFDEDataDir, "ubuntu-save.key")
	reinstallSaveKey := filepath.Join(boot.InstallHostFDEDataDir, "reinstall.key")

	if err := saveKeySet.Key.Save(saveKey); err != nil {
		return fmt.Errorf("cannot store system save key: %v", err)
	}
	if err := saveKeySet.RecoveryKey.Save(reinstallSaveKey); err != nil {
		return fmt.Errorf("cannot store reinstall key: %v", err)
	}
	return nil
}

var secbootCheckTPMKeySealingSupported = secboot.CheckTPMKeySealingSupported

// checkEncryption verifies whether encryption should be used based on the
// model grade and the availability of a TPM device or a fde-setup hook
// in the kernel.
func (m *DeviceManager) checkEncryption(st *state.State, deviceCtx snapstate.DeviceContext) (res secboot.EncryptionType, err error) {
	model := deviceCtx.Model()
	secured := model.Grade() == asserts.ModelSecured
	dangerous := model.Grade() == asserts.ModelDangerous
	encrypted := model.StorageSafety() == asserts.StorageSafetyEncrypted

	// check if we should disable encryption non-secured devices
	// TODO:UC20: this is not the final mechanism to bypass encryption
	if dangerous && osutil.FileExists(filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")) {
		return res, nil
	}

	// check if the model prefers to be unencrypted
	// TODO: provide way to select via install chooser menu
	//       if the install is unencrypted or encrypted
	if model.StorageSafety() == asserts.StorageSafetyPreferUnencrypted {
		logger.Noticef(`installing system unencrypted to comply with prefer-unencrypted storage-safety model option`)
		return res, nil
	}

	// check if encryption is available
	var (
		hasFDESetupHook    bool
		checkEncryptionErr error
	)
	if kernelInfo, err := snapstate.KernelInfo(st, deviceCtx); err == nil {
		if hasFDESetupHook = hasFDESetupHookInKernel(kernelInfo); hasFDESetupHook {
			res, checkEncryptionErr = m.checkFDEFeatures()
		}
	}
	// Note that having a fde-setup hook will disable the build-in
	// secboot encryption
	if !hasFDESetupHook {
		checkEncryptionErr = secbootCheckTPMKeySealingSupported()
		if checkEncryptionErr == nil {
			res = secboot.EncryptionTypeLUKS
		}
	}

	// check if encryption is required
	if checkEncryptionErr != nil {
		if secured {
			return res, fmt.Errorf("cannot encrypt device storage as mandated by model grade secured: %v", checkEncryptionErr)
		}
		if encrypted {
			return res, fmt.Errorf("cannot encrypt device storage as mandated by encrypted storage-safety model option: %v", checkEncryptionErr)
		}

		if hasFDESetupHook {
			logger.Noticef("not encrypting device storage as querying kernel fde-setup hook did not succeed: %v", checkEncryptionErr)
		} else {
			logger.Noticef("not encrypting device storage as checking TPM gave: %v", checkEncryptionErr)
		}

		// not required, go without
		return res, nil
	}

	return res, nil
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

	// ensure the next boot goes into run mode
	if err := bootEnsureNextBootToRunMode(modeEnv.RecoverySystem); err != nil {
		return err
	}

	var rebootOpts RebootOptions
	err = t.Get("reboot", &rebootOpts)
	if err != nil && err != state.ErrNoState {
		return err
	}

	// write timing information
	if err := writeTimings(st, boot.InstallHostWritableDir, modeEnv.Mode); err != nil {
		logger.Noticef("cannot write timings: %v", err)
	}
	// store install-mode log into ubuntu-data partition
	if err := writeLogs(boot.InstallHostWritableDir, modeEnv.Mode); err != nil {
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
	encryptionType, err := m.checkEncryption(st, deviceCtx)
	if err != nil {
		return err
	}
	bopts.EncryptionType = encryptionType
	useEncryption := (encryptionType != secboot.EncryptionTypeNone)
	hasMarker := osutil.FileExists(filepath.Join(boot.InstallHostFDESaveDir, "marker"))
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

	// run the create partition code
	logger.Noticef("create and deploy partitions")
	timings.Run(perfTimings, "factory-reset", "Factory reset", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		_, err = installFactoryReset(model, gadgetDir, kernelDir, "", bopts, nil, tm)
	})
	if err != nil {
		return fmt.Errorf("cannot perform factory reset: %v", err)
	}

	// TODO: splice into a common install/reset helper?

	// keep track of the model we installed
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}
	err = writeModel(model, filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}

	// preserve systemd-timesyncd clock timestamp, so that RTC-less devices
	// can start with a more recent time on the next boot
	if err := writeTimesyncdClock(dirs.GlobalRootDir, boot.InstallHostWritableDir); err != nil {
		return fmt.Errorf("cannot seed timesyncd clock: %v", err)
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir, GadgetDir: gadgetDir}
	// configure cloud init
	setSysconfigCloudOptions(opts, gadgetDir, model)
	timings.Run(perfTimings, "sysconfig-configure-target-system", "Configure target system", func(timings.Measurer) {
		err = sysconfigConfigureTargetSystem(model, opts)
	})
	if err != nil {
		return err
	}

	if err := restoreDeviceFromSave(model); err != nil {
		return fmt.Errorf("cannot restore data from save: %v", err)
	}

	// on some specific devices, we need to create these directories in
	// _writable_defaults in order to allow the install-device hook to install
	// some files there, this eventually will go away when we introduce a proper
	// mechanism not using system-files to install files onto the root
	// filesystem from the install-device hook
	if err := fixupWritableDefaultDirs(boot.InstallHostWritableDir); err != nil {
		return err
	}

	// make it bootable
	logger.Noticef("make system runnable")
	bootBaseInfo, err := snapstate.BootBaseInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get boot base info: %v", err)
	}
	recoverySystemDir := filepath.Join("/systems", modeEnv.RecoverySystem)
	bootWith := &boot.BootableSet{
		Base:              bootBaseInfo,
		BasePath:          bootBaseInfo.MountFile(),
		Kernel:            kernelInfo,
		KernelPath:        kernelInfo.MountFile(),
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: gadgetDir,
	}
	timings.Run(perfTimings, "boot-make-runnable", "Make target system runnable", func(timings.Measurer) {
		err = bootMakeRunnable(deviceCtx.Model(), bootWith, nil)
	})
	if err != nil {
		return fmt.Errorf("cannot make system runnable: %v", err)
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
	if (err != nil && asserts.IsNotFound(err)) || len(serials) == 0 {
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

	toDB, err := sysdb.OpenAt(filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/assertions"))
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
