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
	"bytes"
	"compress/gzip"
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
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
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
)

var (
	bootMakeBootablePartition            = boot.MakeBootablePartition
	bootMakeRunnable                     = boot.MakeRunnableSystem
	bootMakeRunnableAfterDataReset       = boot.MakeRunnableSystemAfterDataReset
	bootEnsureNextBootToRunMode          = boot.EnsureNextBootToRunMode
	installRun                           = install.Run
	installFactoryReset                  = install.FactoryReset
	installMountVolumes                  = install.MountVolumes
	installWriteContent                  = install.WriteContent
	installEncryptPartitions             = install.EncryptPartitions
	secbootStageEncryptionKeyChange      = secboot.StageEncryptionKeyChange
	secbootTransitionEncryptionKeyChange = secboot.TransitionEncryptionKeyChange

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

// buildInstallObserver creates an observer for gadget assets if
// applicable, otherwise the returned gadget.ContentObserver is nil.
// The observer if any is also returned as non-nil trustedObserver if
// encryption is in use.
func buildInstallObserver(model *asserts.Model, gadgetDir string, useEncryption bool) (
	observer gadget.ContentObserver, trustedObserver *boot.TrustedAssetsInstallObserver, err error) {

	// observer will be a nil interface by default
	trustedObserver, err = boot.TrustedAssetsInstallObserverForModel(model, gadgetDir, useEncryption)
	if err != nil && err != boot.ErrObserverNotApplicable {
		return nil, nil, fmt.Errorf("cannot setup asset install observer: %v", err)
	}
	if err == nil {
		observer = trustedObserver
		if !useEncryption {
			// there will be no key sealing, so past the
			// installation pass no other methods need to be called
			trustedObserver = nil
		}
	}

	return observer, trustedObserver, nil
}

func (m *DeviceManager) doSetupUbuntuSave(t *state.Task, _ *tomb.Tomb) error {
	return m.setupUbuntuSave()
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

	installObserver, trustedInstallObserver, err := buildInstallObserver(model, gadgetDir, useEncryption)
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
		if err := prepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	if err := prepareRunSystemData(model, gadgetDir, perfTimings); err != nil {
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

func prepareEncryptedSystemData(model *asserts.Model, keyForRole map[string]keys.EncryptionKey, trustedInstallObserver *boot.TrustedAssetsInstallObserver) error {
	// validity check
	if len(keyForRole) == 0 || keyForRole[gadget.SystemData] == nil || keyForRole[gadget.SystemSave] == nil {
		return fmt.Errorf("internal error: system encryption keys are unset")
	}
	dataEncryptionKey := keyForRole[gadget.SystemData]
	saveEncryptionKey := keyForRole[gadget.SystemSave]

	// make note of the encryption keys
	trustedInstallObserver.ChosenEncryptionKeys(dataEncryptionKey, saveEncryptionKey)

	// keep track of recovery assets
	if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
		return fmt.Errorf("cannot observe existing trusted recovery assets: err")
	}
	if err := saveKeys(model, keyForRole); err != nil {
		return err
	}
	// write markers containing a secret to pair data and save
	if err := writeMarkers(model); err != nil {
		return err
	}
	return nil
}

func prepareRunSystemData(model *asserts.Model, gadgetDir string, perfTimings timings.Measurer) error {
	// keep track of the model we installed
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}
	err = writeModel(model, filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}

	// preserve systemd-timesyncd clock timestamp, so that RTC-less devices
	// can start with a more recent time on the next boot
	if err := writeTimesyncdClock(dirs.GlobalRootDir, boot.InstallHostWritableDir(model)); err != nil {
		return fmt.Errorf("cannot seed timesyncd clock: %v", err)
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir(model), GadgetDir: gadgetDir}
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

	if !model.Classic() {
		// on some specific devices, we need to create these directories in
		// _writable_defaults in order to allow the install-device hook to install
		// some files there, this eventually will go away when we introduce a proper
		// mechanism not using system-files to install files onto the root
		// filesystem from the install-device hook
		if err := fixupWritableDefaultDirs(boot.InstallHostWritableDir(model)); err != nil {
			return err
		}
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

	for _, subDirToCreate := range []string{"/etc/udev/rules.d", "/etc/modprobe.d", "/etc/modules-load.d/", "/etc/systemd/network"} {
		dirToCreate := sysconfig.WritableDefaultsDir(systemDataDir, subDirToCreate)

		if err := os.MkdirAll(dirToCreate, 0755); err != nil {
			return err
		}
	}

	return nil
}

// writeMarkers writes markers containing the same secret to pair data and save.
func writeMarkers(model *asserts.Model) error {
	// ensure directory for markers exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir(model), 0755); err != nil {
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

	return device.WriteEncryptionMarkers(boot.InstallHostFDEDataDir(model), boot.InstallHostFDESaveDir, markerSecret)
}

func saveKeys(model *asserts.Model, keyForRole map[string]keys.EncryptionKey) error {
	saveEncryptionKey := keyForRole[gadget.SystemSave]
	if saveEncryptionKey == nil {
		// no system-save support
		return nil
	}
	// ensure directory for keys exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir(model), 0755); err != nil {
		return err
	}
	if err := saveEncryptionKey.Save(device.SaveKeyUnder(boot.InstallHostFDEDataDir(model))); err != nil {
		return fmt.Errorf("cannot store system save key: %v", err)
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

	preseeded, err := maybeApplyPreseededData(st, boot.InitramfsUbuntuSeedDir, modeEnv.RecoverySystem, boot.InstallHostWritableDir(model))
	if err != nil {
		logger.Noticef("failed to apply preseed data: %v", err)
		return err
	}
	if preseeded {
		logger.Noticef("successfully preseeded the system")
	} else {
		logger.Noticef("preseed data not present, will do normal seeding")
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

func readPreseedAssertion(st *state.State, model *asserts.Model, ubuntuSeedDir, sysLabel string) (*asserts.Preseed, error) {
	f, err := os.Open(filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed"))
	if err != nil {
		return nil, fmt.Errorf("cannot read preseed assertion: %v", err)
	}

	// main seed assertions are loaded in the assertion db of install mode; add preseed assertion from
	// systems/<label>/preseed file on top of it via a temporary db.
	tmpDb := assertstate.TemporaryDB(st)
	batch := asserts.NewBatch(nil)
	_, err = batch.AddStream(f)
	if err != nil {
		return nil, err
	}

	var preseedAs *asserts.Preseed
	err = batch.CommitToAndObserve(tmpDb, func(as asserts.Assertion) {
		if as.Type() == asserts.PreseedType {
			preseedAs = as.(*asserts.Preseed)
		}
	}, nil)
	if err != nil {
		return nil, err
	}

	switch {
	case preseedAs == nil:
		return nil, fmt.Errorf("internal error: preseed assertion file is present but preseed assertion not found")
	case preseedAs.SystemLabel() != sysLabel:
		return nil, fmt.Errorf("preseed assertion system label %q doesn't match system label %q", preseedAs.SystemLabel(), sysLabel)
	case preseedAs.Model() != model.Model():
		return nil, fmt.Errorf("preseed assertion model %q doesn't match the model %q", preseedAs.Model(), model.Model())
	case preseedAs.BrandID() != model.BrandID():
		return nil, fmt.Errorf("preseed assertion brand %q doesn't match model brand %q", preseedAs.BrandID(), model.BrandID())
	case preseedAs.Series() != model.Series():
		return nil, fmt.Errorf("preseed assertion series %q doesn't match model series %q", preseedAs.Series(), model.Series())
	}

	return preseedAs, nil
}

var seedOpen = seed.Open

// TODO: consider reusing this kind of handler for UC20 seeding
type preseedSnapHandler struct {
	writableDir string
}

func (p *preseedSnapHandler) HandleUnassertedSnap(name, path string, _ timings.Measurer) (string, error) {
	pinfo := snap.MinimalPlaceInfo(name, snap.Revision{N: -1})
	targetPath := filepath.Join(p.writableDir, pinfo.MountFile())
	mountDir := filepath.Join(p.writableDir, pinfo.MountDir())

	sq := squashfs.New(path)
	opts := &snap.InstallOptions{MustNotCrossDevices: true}
	if _, err := sq.Install(targetPath, mountDir, opts); err != nil {
		return "", fmt.Errorf("cannot install snap %q: %v", name, err)
	}

	return targetPath, nil
}

func (p *preseedSnapHandler) HandleAndDigestAssertedSnap(name, path string, essType snap.Type, snapRev *asserts.SnapRevision, _ func(string, uint64) (snap.Revision, error), _ timings.Measurer) (string, string, uint64, error) {
	pinfo := snap.MinimalPlaceInfo(name, snap.Revision{N: snapRev.SnapRevision()})
	targetPath := filepath.Join(p.writableDir, pinfo.MountFile())
	mountDir := filepath.Join(p.writableDir, pinfo.MountDir())

	logger.Debugf("copying: %q to %q; mount dir=%q", path, targetPath, mountDir)

	srcFile, err := os.Open(path)
	if err != nil {
		return "", "", 0, err
	}
	defer srcFile.Close()

	destFile, err := osutil.NewAtomicFile(targetPath, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot create atomic file: %v", err)
	}
	defer destFile.Cancel()

	finfo, err := srcFile.Stat()
	if err != nil {
		return "", "", 0, err
	}

	destFile.SetModTime(finfo.ModTime())

	h := crypto.SHA3_384.New()
	w := io.MultiWriter(h, destFile)

	size, err := io.CopyBuffer(w, srcFile, make([]byte, 2*1024*1024))
	if err != nil {
		return "", "", 0, err
	}
	if err := destFile.Commit(); err != nil {
		return "", "", 0, fmt.Errorf("cannot copy snap %q: %v", name, err)
	}

	sq := squashfs.New(targetPath)
	opts := &snap.InstallOptions{MustNotCrossDevices: true}
	// since Install target path is the same as source path passed to squashfs.New,
	// Install isn't going to copy the blob, but we call it to set up mount directory etc.
	if _, err := sq.Install(targetPath, mountDir, opts); err != nil {
		return "", "", 0, fmt.Errorf("cannot install snap %q: %v", name, err)
	}

	sha3_384, err := asserts.EncodeDigest(crypto.SHA3_384, h.Sum(nil))
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot encode snap %q digest: %v", path, err)
	}
	return targetPath, sha3_384, uint64(size), nil
}

var maybeApplyPreseededData = func(st *state.State, ubuntuSeedDir, sysLabel, writableDir string) (preseeded bool, err error) {
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	if !osutil.FileExists(preseedArtifact) {
		return false, nil
	}

	model, err := findModel(st)
	if err != nil {
		return false, fmt.Errorf("preseed error: cannot find model: %v", err)
	}

	preseedAs, err := readPreseedAssertion(st, model, ubuntuSeedDir, sysLabel)
	if err != nil {
		return false, err
	}

	// TODO: consider a writer that feeds the file to stdin of tar and calculates the digest at the same time.
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	if err != nil {
		return false, fmt.Errorf("cannot calculate preseed artifact digest: %v", err)
	}

	digest, err := base64.RawURLEncoding.DecodeString(preseedAs.ArtifactSHA3_384())
	if err != nil {
		return false, fmt.Errorf("cannot decode preseed artifact digest")
	}
	if !bytes.Equal(sha3_384, digest) {
		return false, fmt.Errorf("invalid preseed artifact digest")
	}

	logger.Noticef("apply preseed data: %q, %q", writableDir, preseedArtifact)
	cmd := exec.Command("tar", "--extract", "--preserve-permissions", "--preserve-order", "--gunzip", "--directory", writableDir, "-f", preseedArtifact)
	if err := cmd.Run(); err != nil {
		return false, err
	}

	logger.Noticef("copying snaps")

	deviceSeed, err := seedOpen(ubuntuSeedDir, sysLabel)
	if err != nil {
		return false, err
	}
	tm := timings.New(nil)

	if err := deviceSeed.LoadAssertions(nil, nil); err != nil {
		return false, err
	}

	if err := os.MkdirAll(filepath.Join(writableDir, "var/lib/snapd/snaps"), 0755); err != nil {
		return false, err
	}

	snapHandler := &preseedSnapHandler{writableDir: writableDir}
	if err := deviceSeed.LoadMeta("run", snapHandler, tm); err != nil {
		return false, err
	}

	preseedSnaps := make(map[string]*asserts.PreseedSnap)
	for _, ps := range preseedAs.Snaps() {
		preseedSnaps[ps.Name] = ps
	}

	checkSnap := func(ssnap *seed.Snap) error {
		ps, ok := preseedSnaps[ssnap.SnapName()]
		if !ok {
			return fmt.Errorf("snap %q not present in the preseed assertion", ssnap.SnapName())
		}
		if ps.Revision != ssnap.SideInfo.Revision.N {
			rev := snap.Revision{N: ps.Revision}
			return fmt.Errorf("snap %q has wrong revision %s (expected: %s)", ssnap.SnapName(), ssnap.SideInfo.Revision, rev)
		}
		if ps.SnapID != ssnap.SideInfo.SnapID {
			return fmt.Errorf("snap %q has wrong snap id %q (expected: %q)", ssnap.SnapName(), ssnap.SideInfo.SnapID, ps.SnapID)
		}
		return nil
	}

	esnaps := deviceSeed.EssentialSnaps()
	msnaps, err := deviceSeed.ModeSnaps("run")
	if err != nil {
		return false, err
	}
	if len(msnaps)+len(esnaps) != len(preseedSnaps) {
		return false, fmt.Errorf("seed has %d snaps but %d snaps are required by preseed assertion", len(msnaps)+len(esnaps), len(preseedSnaps))
	}

	for _, esnap := range esnaps {
		if err := checkSnap(esnap); err != nil {
			return false, err
		}
	}

	for _, ssnap := range msnaps {
		if err := checkSnap(ssnap); err != nil {
			return false, err
		}
	}

	return true, nil
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

		if err := prepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	if err := prepareRunSystemData(model, gadgetDir, perfTimings); err != nil {
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
	kd, err := ioutil.ReadFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"))
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

func (m *DeviceManager) loadAndMountSystemLabelSnaps(systemLabel string) (
	*System, map[snap.Type]*snap.Info, map[snap.Type]*seed.Snap, map[snap.Type]string, func(), error) {

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
			errUnmount := unmountF()
			if errUnmount != nil {
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
// TODO this needs unit tests
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
	if sys.Model.Grade() != asserts.ModelDangerous && encryptSetupData == nil {
		return fmt.Errorf("grade is %s but encryption has not been set-up", sys.Model.Grade())
	}
	useEncryption := encryptSetupData != nil

	logger.Debugf("starting install-finish for %q (using encryption: %t) on %v", systemLabel, useEncryption, onVolumes)

	// TODO we probably want to pass a different location for the assets cache
	installObserver, trustedInstallObserver, err := buildInstallObserver(sys.Model, mntPtForType[snap.TypeGadget], useEncryption)
	if err != nil {
		return err
	}

	logger.Debugf("writing content to partitions")
	timings.Run(perfTimings, "install-content", "Writing content to partitions", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		_, err = installWriteContent(onVolumes, installObserver,
			mntPtForType[snap.TypeGadget], mntPtForType[snap.TypeKernel], sys.Model, encryptSetupData, perfTimings)
	})
	if err != nil {
		return fmt.Errorf("cannot write content: %v", err)
	}

	// Mount the partitions and find ESP partition
	espMntDir, unmountParts, err := installMountVolumes(onVolumes, encryptSetupData)
	if err != nil {
		return fmt.Errorf("cannot mount partitions for installation: %v", err)
	}
	defer unmountParts()

	if useEncryption {
		if err := install.FinishEncryption(sys.Model, encryptSetupData); err != nil {
			return fmt.Errorf("cannot finish encryption: %v", err)
		}
		if trustedInstallObserver != nil {
			if err := prepareEncryptedSystemData(sys.Model, install.KeysForRole(encryptSetupData), trustedInstallObserver); err != nil {
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

	// installs in ESP: grub.cfg, grubenv
	logger.Debugf("making the ESP partition bootable, mount dir is %q", espMntDir)
	opts := &bootloader.Options{
		PrepareImageTime: false,
		// We need the same configuration that a recovery partition,
		// as we will chainload to grub in the boot partition.
		Role: bootloader.RoleRecovery,
	}
	if err := bootMakeBootablePartition(espMntDir, opts, bootWith, boot.ModeRun, nil); err != nil {
		return err
	}

	// writes the model
	if err := prepareRunSystemData(sys.Model, bootWith.UnpackedGadgetDir, perfTimings); err != nil {
		return err
	}

	logger.Debugf("making the installed system runnable for systemLabel %s", systemLabel)
	if err := bootMakeRunnable(sys.Model, bootWith, trustedInstallObserver); err != nil {
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

	encryptInfo, err := m.encryptionSupportInfo(sys.Model, snapInfos[snap.TypeKernel], gadgetInfo)
	if err != nil {
		return err
	}
	if encryptInfo.Type == secboot.EncryptionTypeNone {
		return fmt.Errorf("encryption unavailable on this device")
	}

	// FIXME this is fixed to LUKS at the moment
	encryptionSetupData, err := installEncryptPartitions(onVolumes, mntPtForType[snap.TypeGadget],
		mntPtForType[snap.TypeKernel], sys.Model, secboot.EncryptionTypeLUKS, perfTimings)
	if err != nil {
		return err
	}

	// Store created devices in the change so they can be accessed from the installer
	apiData := make(map[string]interface{})
	ch := t.Change()
	for _, p := range encryptionSetupData.Parts {
		// key: partition role, value: mapper device
		apiData[p.Role] = p.EncryptedDevice
	}
	ch.Set("api-data", apiData)

	st.Cache(encryptionSetupDataKey{systemLabel}, encryptionSetupData)

	return nil
}
