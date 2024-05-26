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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	_ "golang.org/x/crypto/sha3"
	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(os.MkdirAll(filepath.Dir(logPath), 0755))

	f := mylog.Check2(os.Create(logPath))

	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	cmd := exec.Command("journalctl", "-b", "0", "--all")
	cmd.Stdout = gz
	mylog.Check(cmd.Run())
	mylog.Check(gz.Flush())

	return nil
}

func writeTimings(st *state.State, rootdir, fromMode string) error {
	changeKind := "install-system"
	logPath := filepath.Join(rootdir, "var/log/install-timings.txt.gz")
	if fromMode == "factory-reset" {
		changeKind = "factory-reset"
		logPath = filepath.Join(rootdir, "var/log/factory-reset-timings.txt.gz")
	}
	mylog.Check(os.MkdirAll(filepath.Dir(logPath), 0755))

	f := mylog.Check2(os.Create(logPath))

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
	mylog.Check(cmd.Run())

	fmt.Fprintf(gz, "\n")
	// then the seeding
	fmt.Fprintf(gz, "---- Output of snap debug timings --ensure=seed\n")
	cmd = exec.Command("snap", "debug", "timings", "--ensure=seed")
	cmd.Stdout = gz
	mylog.Check(cmd.Run())

	fmt.Fprintf(gz, "\n")
	// then the install
	fmt.Fprintf(gz, "---- Output of snap debug timings --ensure=%v\n", changeKind)
	cmd = exec.Command("snap", "debug", "timings", fmt.Sprintf("--ensure=%v", changeKind))
	cmd.Stdout = gz
	mylog.Check(cmd.Run())

	// then the other changes (if there are any)
	for _, chgID := range chgIDs {
		fmt.Fprintf(gz, "---- Output of snap debug timings %s\n", chgID)
		cmd = exec.Command("snap", "debug", "timings", chgID)
		cmd.Stdout = gz
		mylog.Check(cmd.Run())

		fmt.Fprintf(gz, "\n")
	}
	mylog.Check(gz.Flush())

	return nil
}

func (m *DeviceManager) doSetupUbuntuSave(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	deviceCtx := mylog.Check2(DeviceCtx(st, t, nil))

	return m.setupUbuntuSave(deviceCtx)
}

func (m *DeviceManager) doSetupRunSystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	// get gadget dir
	deviceCtx := mylog.Check2(DeviceCtx(st, t, nil))

	gadgetInfo := mylog.Check2(snapstate.GadgetInfo(st, deviceCtx))

	gadgetDir := gadgetInfo.MountDir()

	kernelInfo := mylog.Check2(snapstate.KernelInfo(st, deviceCtx))

	kernelDir := kernelInfo.MountDir()

	modeEnv := mylog.Check2(maybeReadModeenv())

	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	// bootstrap
	bopts := install.Options{
		Mount: true,
	}
	encryptionType := mylog.Check2(m.checkEncryption(st, deviceCtx, secboot.TPMProvisionFull))

	bopts.EncryptionType = encryptionType
	useEncryption := (encryptionType != secboot.EncryptionTypeNone)

	model := deviceCtx.Model()

	// make sure that gadget is usable for the set up we want to use it in
	validationConstraints := gadget.ValidationConstraints{
		EncryptedData: useEncryption,
	}
	var ginfo *gadget.Info
	timings.Run(perfTimings, "read-info-and-validate", "Read and validate gagdet info", func(timings.Measurer) {
		ginfo = mylog.Check2(gadget.ReadInfoAndValidate(gadgetDir, model, &validationConstraints))
	})
	mylog.Check(gadget.ValidateContent(ginfo, gadgetDir, kernelDir))

	installObserver, trustedInstallObserver := mylog.Check3(installLogic.BuildInstallObserver(model, gadgetDir, useEncryption))

	var installedSystem *install.InstalledSystemSideData
	// run the create partition code
	logger.Noticef("create and deploy partitions")
	timings.Run(perfTimings, "install-run", "Install the run system", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		installedSystem = mylog.Check2(installRun(model, gadgetDir, kernelDir, "", bopts, installObserver, tm))
	})

	if trustedInstallObserver != nil {
		mylog.Check(installLogic.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver))
	}
	mylog.Check(installLogicPrepareRunSystemData(model, gadgetDir, perfTimings))

	// make it bootable, which should be the final step in the process, as
	// it effectively makes it possible to boot into run mode
	logger.Noticef("make system runnable")
	bootBaseInfo := mylog.Check2(snapstate.BootBaseInfo(st, deviceCtx))

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
		mylog.Check(bootMakeRunnable(deviceCtx.Model(), bootWith, trustedInstallObserver))
	})

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

	modeEnv := mylog.Check2(maybeReadModeenv())

	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	deviceCtx := mylog.Check2(DeviceCtx(st, t, nil))

	model := deviceCtx.Model()

	preseeded := mylog.Check2(maybeApplyPreseededData(model, boot.InitramfsUbuntuSeedDir, modeEnv.RecoverySystem, boot.InstallHostWritableDir(model)))

	if preseeded {
		logger.Noticef("successfully preseeded the system")
	} else {
		logger.Noticef("preseed data not present, will do normal seeding")
	}

	// if the model has a gadget snap, and said gadget snap has an install-device hook
	// call systemctl daemon-reload to account for any potential side-effects of that
	// install-device hook
	hasHook := mylog.Check2(m.hasInstallDeviceHook(model))

	if hasHook {
		sd := systemd.New(systemd.SystemMode, progress.Null)
		mylog.Check(sd.DaemonReload())

	}
	mylog.Check(

		// ensure the next boot goes into run mode
		bootEnsureNextBootToRunMode(modeEnv.RecoverySystem))

	var rebootOpts RebootOptions
	mylog.Check(t.Get("reboot", &rebootOpts))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	mylog.Check(

		// write timing information
		writeTimings(st, boot.InstallHostWritableDir(model), modeEnv.Mode))
	mylog.Check(

		// store install-mode log into ubuntu-data partition
		writeLogs(boot.InstallHostWritableDir(model), modeEnv.Mode))

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
	sysSeed := mylog.Check2(seedOpen(ubuntuSeedDir, sysLabel))

	// this function is for UC20+ only so sysSeed ia always PreseedCapable
	preseedSeed := sysSeed.(seed.PreseedCapable)

	if !preseedSeed.HasArtifact("preseed.tgz") {
		return false, nil
	}
	mylog.Check(preseedSeed.LoadAssertions(nil, nil))

	_, sig := model.Signature()
	_, seedModelSig := preseedSeed.Model().Signature()
	if !bytes.Equal(sig, seedModelSig) {
		return false, fmt.Errorf("system seed %q model does not match model in use", sysLabel)
	}
	mylog.Check(applyPreseededData(preseedSeed, writableDir))

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
	deviceCtx := mylog.Check2(DeviceCtx(st, t, nil))

	gadgetInfo := mylog.Check2(snapstate.GadgetInfo(st, deviceCtx))

	gadgetDir := gadgetInfo.MountDir()

	kernelInfo := mylog.Check2(snapstate.KernelInfo(st, deviceCtx))

	kernelDir := kernelInfo.MountDir()

	modeEnv := mylog.Check2(maybeReadModeenv())

	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	// bootstrap
	bopts := install.Options{
		Mount: true,
	}
	encryptionType := mylog.Check2(m.checkEncryption(st, deviceCtx, secboot.TPMPartialReprovision))

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
		ginfo = mylog.Check2(gadget.ReadInfoAndValidate(gadgetDir, model, &validationConstraints))
	})
	mylog.Check(gadget.ValidateContent(ginfo, gadgetDir, kernelDir))

	var trustedInstallObserver *boot.TrustedAssetsInstallObserver
	// get a nice nil interface by default
	var installObserver gadget.ContentObserver
	trustedInstallObserver = mylog.Check2(boot.TrustedAssetsInstallObserverForModel(model, gadgetDir, useEncryption))
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
		installedSystem = mylog.Check2(installFactoryReset(model, gadgetDir, kernelDir, "", bopts, installObserver, tm))
	})

	logger.Noticef("devs: %+v", installedSystem.DeviceForRole)

	if trustedInstallObserver != nil {
		mylog.
			// at this point we removed boot and data. sealed fallback key
			// for ubuntu-data is becoming useless
			Check(os.Remove(device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot cleanup obsolete key file: %v", err)
		}
		mylog.Check(

			// it is possible that we reached this place again where a
			// previously running factory reset was interrupted by a reboot
			os.Remove(device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot cleanup obsolete key file: %v", err)
		}
		mylog.Check(

			// it is ok if the recovery key file on disk does not exist;
			// ubuntu-save was opened during boot, so the removal operation
			// can be authorized with a key from the keyring
			secbootRemoveRecoveryKeys(map[secboot.RecoveryKeyDevice]string{
				{Mountpoint: boot.InitramfsUbuntuSaveDir}: device.RecoveryKeyUnder(boot.InstallHostFDEDataDir(model)),
			}))

		// new encryption key for save
		saveEncryptionKey := mylog.Check2(keys.NewEncryptionKey())

		saveNode := installedSystem.DeviceForRole[gadget.SystemSave]
		if saveNode == "" {
			return fmt.Errorf("internal error: no system-save device")
		}
		mylog.Check(secbootStageEncryptionKeyChange(saveNode, saveEncryptionKey))

		// keep track of the new ubuntu-save encryption key
		installedSystem.KeyForRole[gadget.SystemSave] = saveEncryptionKey
		mylog.Check(installLogic.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver))

	}
	mylog.Check(installLogicPrepareRunSystemData(model, gadgetDir, perfTimings))
	mylog.Check(restoreDeviceFromSave(model))

	// make it bootable
	logger.Noticef("make system runnable")
	bootBaseInfo := mylog.Check2(snapstate.BootBaseInfo(st, deviceCtx))

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
		mylog.Check(bootMakeRunnableAfterDataReset(deviceCtx.Model(), bootWith, trustedInstallObserver))
	})

	// leave a marker that factory reset was performed
	factoryResetMarker := filepath.Join(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir(model)), "factory-reset")
	mylog.Check(writeFactoryResetMarker(factoryResetMarker, useEncryption))

	return nil
}

func restoreDeviceFromSave(model *asserts.Model) error {
	// we could also look at factory-reset-bootstrap.json left by
	// snap-bootstrap, but the mount was already verified during boot
	mounted := mylog.Check2(osutil.IsMounted(boot.InitramfsUbuntuSaveDir))

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
	fromDB := mylog.Check2(sysdb.OpenAt(fromDevice))

	// key pair manager always uses ubuntu-save whenever it's available
	kp := mylog.Check2(asserts.OpenFSKeypairManager(fromDevice))

	// there should be a serial assertion for the current model
	serials := mylog.Check2(fromDB.FindMany(asserts.SerialType, map[string]string{
		"brand-id": model.BrandID(),
		"model":    model.Model(),
	}))
	if (err != nil && errors.Is(err, &asserts.NotFoundError{})) || len(serials) == 0 {
		// there is no serial assertion in the old system that matches
		// our model, it is still possible that the old system could
		// have generated device keys and sent out a serial request, but
		// for simplicity we ignore this scenario and a new set of keys
		// will be generated after booting into the run system
		logger.Debugf("no serial assertion for %v/%v", model.BrandID(), model.Model())
		return nil
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
		_ = mylog.Check2(kp.Get(deviceKeyID))

	}

	if serialAs == nil {
		// no serial assertion that matches the model, brand and is
		// signed with a device key that is present in the filesystem
		logger.Debugf("no valid serial assertions")
		return nil
	}

	logger.Debugf("found a serial assertion for %v/%v, with serial %v",
		model.BrandID(), model.Model(), serialAs.Serial())

	toDB := mylog.Check2(sysdb.OpenAt(filepath.Join(boot.InstallHostWritableDir(model), "var/lib/snapd/assertions")))

	logger.Debugf("importing serial and model assertions")
	b := asserts.NewBatch(nil)
	mylog.Check(b.Fetch(toDB,
		func(ref *asserts.Ref) (asserts.Assertion, error) { return ref.Resolve(fromDB.Find) },
		func(f asserts.Fetcher) error {
			mylog.Check(f.Save(model))

			return f.Save(serialAs)
		}))
	mylog.Check(b.CommitTo(toDB, nil))

	return nil
}

type factoryResetMarker struct {
	FallbackSaveKeyHash string `json:"fallback-save-key-sha3-384,omitempty"`
}

func fileDigest(p string) (string, error) {
	digest, _ := mylog.Check3(osutil.FileDigest(p, crypto.SHA3_384))

	return hex.EncodeToString(digest), nil
}

func writeFactoryResetMarker(marker string, hasEncryption bool) error {
	keyDigest := ""
	if hasEncryption {
		d := mylog.Check2(fileDigest(device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)))

		keyDigest = d
	}
	var buf bytes.Buffer
	mylog.Check(json.NewEncoder(&buf).Encode(factoryResetMarker{
		FallbackSaveKeyHash: keyDigest,
	}))

	if hasEncryption {
		logger.Noticef("writing factory-reset marker at %v with key digest %q", marker, keyDigest)
	} else {
		logger.Noticef("writing factory-reset marker at %v", marker)
	}
	return osutil.AtomicWriteFile(marker, buf.Bytes(), 0644, 0)
}

func verifyFactoryResetMarkerInRun(marker string, hasEncryption bool) error {
	f := mylog.Check2(os.Open(marker))

	defer f.Close()
	var frm factoryResetMarker
	mylog.Check(json.NewDecoder(f).Decode(&frm))

	if hasEncryption {
		saveFallbackKeyFactory := device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
		d := mylog.Check2(fileDigest(saveFallbackKeyFactory))

		// possible that there was unexpected reboot
		// before, after the key was moved, but before
		// the marker was removed, in which case the
		// actual fallback key should have the right
		// digest

		// unless it's a different error

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
	kd := mylog.Check2(os.ReadFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key")))
	mylog.Check(

		// does the right thing if the key has already been transitioned
		secbootTransitionEncryptionKeyChange(boot.InitramfsUbuntuSaveDir, keys.EncryptionKey(kd)))

	return nil
}

type encryptionSetupDataKey struct {
	systemLabel string
}

func mountSeedSnap(seedSn *seed.Snap) (mountpoint string, unmount func() error, err error) {
	mountpoint = filepath.Join(dirs.SnapRunDir, "snap-content", string(seedSn.EssentialType))
	mylog.Check(os.MkdirAll(mountpoint, 0755))

	// temporarily mount the filesystem
	logger.Debugf("mounting %q in %q", seedSn.Path, mountpoint)
	sd := systemd.New(systemd.SystemMode, progress.Null)
	mylog.Check(sd.Mount(seedSn.Path, mountpoint))

	return mountpoint,
		func() error {
			logger.Debugf("unmounting %q", mountpoint)
			return sd.Umount(mountpoint)
		},
		nil
}

func (m *DeviceManager) loadAndMountSystemLabelSnaps(systemLabel string) (*systemAndEssentialSnaps, map[snap.Type]string, func(), error) {
	essentialTypes := []snap.Type{snap.TypeKernel, snap.TypeBase, snap.TypeGadget}
	systemAndSnaps := mylog.Check2(m.loadSystemAndEssentialSnaps(systemLabel, essentialTypes))

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
		mntPt, unmountSnap := mylog.Check3(mountSeedSnap(seedSn))

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
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	var systemLabel string
	mylog.Check(t.Get("system-label", &systemLabel))

	var onVolumes map[string]*gadget.Volume
	mylog.Check(t.Get("on-volumes", &onVolumes))

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
	systemAndSnaps, mntPtForType, unmount := mylog.Check4(m.loadAndMountSystemLabelSnaps(systemLabel))
	st.Lock()

	defer unmount()

	// Check if encryption is mandatory
	if systemAndSnaps.Model.StorageSafety() == asserts.StorageSafetyEncrypted && encryptSetupData == nil {
		return fmt.Errorf("storage encryption required by model but has not been set up")
	}
	useEncryption := encryptSetupData != nil

	logger.Debugf("starting install-finish for %q (using encryption: %t) on %v", systemLabel, useEncryption, onVolumes)

	// TODO we probably want to pass a different location for the assets cache
	installObserver, trustedInstallObserver := mylog.Check3(installLogic.BuildInstallObserver(systemAndSnaps.Model, mntPtForType[snap.TypeGadget], useEncryption))

	gi := mylog.Check2(gadget.ReadInfoAndValidate(mntPtForType[snap.TypeGadget], systemAndSnaps.Model, nil))

	// Import new information from the installer to the gadget data,
	// including the target devices and information marked as partial in
	// the gadget, so the gadget is not partially defined anymore if it
	// was.
	// TODO validation of onVolumes versus gadget.yaml, needs to happen here.
	mergedVols := mylog.Check2(gadget.ApplyInstallerVolumesToGadget(onVolumes, gi.Volumes))

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
	volToGadgetToDiskStruct := mylog.Check2(installMatchDisksToGadgetVolumes(mergedVols, volCompatOpts))

	encType := secboot.EncryptionTypeNone
	// TODO:ICE: support secboot.EncryptionTypeLUKSWithICE in the API
	if useEncryption {
		encType = secboot.EncryptionTypeLUKS
	}
	allLaidOutVols := mylog.Check2(gadget.LaidOutVolumesFromGadget(mergedVols,
		mntPtForType[snap.TypeGadget], mntPtForType[snap.TypeKernel],
		encType, volToGadgetToDiskStruct))

	logger.Debugf("writing content to partitions")
	timings.Run(perfTimings, "install-content", "Writing content to partitions", func(tm timings.Measurer) {
		st.Unlock()
		defer st.Lock()
		_ = mylog.Check2(installWriteContent(mergedVols, allLaidOutVols, encryptSetupData, installObserver, perfTimings))
	})

	// Mount the partitions and find the system-seed{,-null} partition
	seedMntDir, unmountParts := mylog.Check3(installMountVolumes(mergedVols, encryptSetupData))

	defer unmountParts()

	if !systemAndSnaps.Model.Classic() {
		copier, ok := systemAndSnaps.Seed.(seed.Copier)
		if !ok {
			return fmt.Errorf("internal error: seed does not support copying: %s", systemAndSnaps.Label)
		}

		logger.Debugf("copying label %q to seed partition", systemAndSnaps.Label)
		mylog.Check(copier.Copy(seedMntDir, systemAndSnaps.Label, perfTimings))

	}
	mylog.Check(installSaveStorageTraits(systemAndSnaps.Model, mergedVols, encryptSetupData))

	if useEncryption {
		if trustedInstallObserver != nil {
			mylog.Check(installLogic.PrepareEncryptedSystemData(systemAndSnaps.Model, install.KeysForRole(encryptSetupData), trustedInstallObserver))
		}
	}

	snapInfos := systemAndSnaps.InfosByType
	snapSeeds := systemAndSnaps.SeedSnapsByType

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
	mylog.Check(bootMakeBootablePartition(seedMntDir, opts, bootWith, boot.ModeRun, nil))
	mylog.Check(

		// writes the model etc
		installLogicPrepareRunSystemData(systemAndSnaps.Model, bootWith.UnpackedGadgetDir, perfTimings))

	logger.Debugf("making the installed system runnable for system label %s", systemLabel)
	mylog.Check(bootMakeRunnableStandalone(systemAndSnaps.Model, bootWith, trustedInstallObserver, st.Unlocker()))

	return nil
}

func (m *DeviceManager) doInstallSetupStorageEncryption(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	var systemLabel string
	mylog.Check(t.Get("system-label", &systemLabel))

	var onVolumes map[string]*gadget.Volume
	mylog.Check(t.Get("on-volumes", &onVolumes))

	logger.Debugf("install-setup-storage-encryption for %q on %v", systemLabel, onVolumes)

	st.Unlock()
	systemAndSeeds, mntPtForType, unmount := mylog.Check4(m.loadAndMountSystemLabelSnaps(systemLabel))
	st.Lock()

	defer unmount()

	// Gadget information
	snapf := mylog.Check2(snapfile.Open(systemAndSeeds.SeedSnapsByType[snap.TypeGadget].Path))

	gadgetInfo := mylog.Check2(gadget.ReadInfoFromSnapFileNoValidate(snapf, systemAndSeeds.Model))

	encryptInfo := mylog.Check2(m.encryptionSupportInfo(systemAndSeeds.Model, secboot.TPMProvisionFull, systemAndSeeds.InfosByType[snap.TypeKernel], gadgetInfo))

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
	encryptionSetupData := mylog.Check2(installEncryptPartitions(onVolumes, encType, systemAndSeeds.Model, mntPtForType[snap.TypeGadget], mntPtForType[snap.TypeKernel], perfTimings))

	// Store created devices in the change so they can be accessed from the installer
	apiData := map[string]interface{}{
		"encrypted-devices": encryptionSetupData.EncryptedDevices(),
	}
	chg := t.Change()
	chg.Set("api-data", apiData)

	st.Cache(encryptionSetupDataKey{systemLabel}, encryptionSetupData)

	return nil
}
