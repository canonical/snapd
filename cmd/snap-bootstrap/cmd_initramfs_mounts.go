// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2024 Canonical Ltd
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

package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	gadgetInstall "github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snapdtool"

	// to set sysconfig.ApplyFilesystemOnlyDefaultsImpl
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/timings"
)

func init() {
	const (
		short = "Generate mounts for the initramfs"
		long  = "Generate and perform all mounts for the initramfs before transitioning to userspace"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("initramfs-mounts", short, long, &cmdInitramfsMounts{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdInitramfsMounts struct{}

func (c *cmdInitramfsMounts) Execute([]string) error {
	boot.HasFDESetupHook = hasFDESetupHook
	boot.RunFDESetupHook = runFDESetupHook

	logger.Noticef("snap-bootstrap version %v starting", snapdtool.Version)

	return generateInitramfsMounts()
}

var (
	osutilIsMounted = osutil.IsMounted

	snapTypeToMountDir = map[snap.Type]string{
		snap.TypeBase:   "base",
		snap.TypeGadget: "gadget",
		snap.TypeKernel: "kernel",
		snap.TypeSnapd:  "snapd",
	}

	secbootProvisionForCVM                       func(initramfsUbuntuSeedDir string) error
	secbootMeasureSnapSystemEpochWhenPossible    func() error
	secbootMeasureSnapModelWhenPossible          func(findModel func() (*asserts.Model, error)) error
	secbootUnlockVolumeUsingSealedKeyIfEncrypted func(disk disks.Disk, name string, encryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error)
	secbootUnlockEncryptedVolumeUsingKey         func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error)

	secbootLockSealedKeys func() error

	bootFindPartitionUUIDForBootedKernelDisk = boot.FindPartitionUUIDForBootedKernelDisk

	mountReadOnlyOptions = &systemdMountOptions{
		ReadOnly: true,
		Private:  true,
	}

	gadgetInstallRun                 = gadgetInstall.Run
	bootMakeRunnableStandaloneSystem = boot.MakeRunnableStandaloneSystemFromInitrd
	installApplyPreseededData        = install.ApplyPreseededData
	bootEnsureNextBootToRunMode      = boot.EnsureNextBootToRunMode
	installBuildInstallObserver      = install.BuildInstallObserver
)

func stampedAction(stamp string, action func() error) error {
	stampFile := filepath.Join(dirs.SnapBootstrapRunDir, stamp)
	if osutil.FileExists(stampFile) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(stampFile), 0755); err != nil {
		return err
	}
	if err := action(); err != nil {
		return err
	}
	return os.WriteFile(stampFile, nil, 0644)
}

func generateInitramfsMounts() (err error) {
	// ensure that the last thing we do is to lock access to sealed keys,
	// regardless of mode or early failures.
	defer func() {
		if e := secbootLockSealedKeys(); e != nil {
			e = fmt.Errorf("error locking access to sealed keys: %v", e)
			if err == nil {
				err = e
			} else {
				// preserve err but log
				logger.Noticef("%v", e)
			}
		}
	}()

	// Ensure there is a very early initial measurement
	err = stampedAction("secboot-epoch-measured", func() error {
		return secbootMeasureSnapSystemEpochWhenPossible()
	})
	if err != nil {
		return err
	}

	mode, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}

	mst := &initramfsMountsState{
		mode:           mode,
		recoverySystem: recoverySystem,
	}
	// generate mounts and set mst.validatedModel
	switch mode {
	case "recover":
		err = generateMountsModeRecover(mst)
	case "install":
		err = generateMountsModeInstall(mst)
	case "factory-reset":
		err = generateMountsModeFactoryReset(mst)
	case "run":
		err = generateMountsModeRun(mst)
	case "cloudimg-rootfs":
		err = generateMountsModeRunCVM(mst)
	default:
		// this should never be reached, ModeAndRecoverySystemFromKernelCommandLine
		// will have returned a non-nill error above if there was another mode
		// specified on the kernel command line for some reason
		return fmt.Errorf("internal error: mode in generateInitramfsMounts not handled")
	}
	if err != nil {
		return err
	}
	model := mst.verifiedModel
	if model == nil {
		return fmt.Errorf("internal error: no verified model set")
	}

	isRunMode := (mode == "run")
	rootfsDir := boot.InitramfsWritableDir(model, isRunMode)

	// finally, the initramfs is responsible for reading the boot flags and
	// copying them to /run, so that userspace has an unambiguous place to read
	// the boot flags for the current boot from
	flags, err := boot.InitramfsActiveBootFlags(mode, rootfsDir)
	if err != nil {
		// We don't die on failing to read boot flags, we just log the error and
		// don't set any flags, this is because the boot flags in the case of
		// install comes from untrusted input, the bootenv. In the case of run
		// mode, boot flags are read from the modeenv, which should be valid and
		// trusted, but if the modeenv becomes corrupted, we would block
		// accessing the system (except through an initramfs shell), to recover
		// the modeenv (though maybe we could enable some sort of fixing from
		// recover mode instead?)
		logger.Noticef("error accessing boot flags: %v", err)
	} else {
		// write the boot flags
		if err := boot.InitramfsExposeBootFlagsForSystem(flags); err != nil {
			// cannot write to /run, error here since arguably we have major
			// problems if we can't write to /run
			return err
		}
	}

	return nil
}

func canInstallAndRunAtOnce(mst *initramfsMountsState) (bool, error) {
	currentSeed, err := mst.LoadSeed(mst.recoverySystem)
	if err != nil {
		return false, err
	}
	preseedSeed, ok := currentSeed.(seed.PreseedCapable)
	if !ok {
		return false, nil
	}

	// TODO: relax this condition when "install and run" well tested
	if !preseedSeed.HasArtifact("preseed.tgz") {
		return false, nil
	}

	// If kernel has fde-setup hook, then we should also have fde-setup in initramfs
	kernelPath := filepath.Join(boot.InitramfsRunMntDir, "kernel")
	kernelHasFdeSetup := osutil.FileExists(filepath.Join(kernelPath, "meta", "hooks", "fde-setup"))
	_, fdeSetupErr := exec.LookPath("fde-setup")
	if kernelHasFdeSetup && fdeSetupErr != nil {
		return false, nil
	}

	gadgetPath := filepath.Join(boot.InitramfsRunMntDir, "gadget")
	if osutil.FileExists(filepath.Join(gadgetPath, "meta", "hooks", "install-device")) {
		return false, nil
	}

	return true, nil
}

func readSnapInfo(sysSnaps map[snap.Type]*seed.Snap, snapType snap.Type) (*snap.Info, error) {
	seedSnap := sysSnaps[snapType]
	mountPoint := filepath.Join(boot.InitramfsRunMntDir, snapTypeToMountDir[snapType])
	info, err := snap.ReadInfoFromMountPoint(seedSnap.SnapName(), mountPoint, seedSnap.Path, seedSnap.SideInfo)
	if err != nil {
		return nil, err
	}
	if info.Revision.Unset() {
		info.Revision = snap.R(-1)
	}
	return info, nil

}

func runFDESetupHook(req *fde.SetupRequest) ([]byte, error) {
	// TODO: use systemd-run
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("fde-setup")
	cmd.Stdin = bytes.NewBuffer(encoded)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}
func hasFDESetupHook(kernelInfo *snap.Info) (bool, error) {
	_, ok := kernelInfo.Hooks["fde-setup"]
	return ok, nil
}

func doInstall(mst *initramfsMountsState, model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap) error {
	kernelSnap, err := readSnapInfo(sysSnaps, snap.TypeKernel)
	if err != nil {
		return err
	}
	baseSnap, err := readSnapInfo(sysSnaps, snap.TypeBase)
	if err != nil {
		return err
	}
	gadgetSnap, err := readSnapInfo(sysSnaps, snap.TypeGadget)
	if err != nil {
		return err
	}
	kernelMountDir := filepath.Join(boot.InitramfsRunMntDir, snapTypeToMountDir[snap.TypeKernel])
	gadgetMountDir := filepath.Join(boot.InitramfsRunMntDir, snapTypeToMountDir[snap.TypeGadget])
	gadgetInfo, err := gadget.ReadInfo(gadgetMountDir, model)
	if err != nil {
		return err
	}
	encryptionSupport, err := install.CheckEncryptionSupport(model, secboot.TPMProvisionFull, kernelSnap, gadgetInfo, runFDESetupHook)
	if err != nil {
		return err
	}
	useEncryption := (encryptionSupport != secboot.EncryptionTypeNone)

	installObserver, trustedInstallObserver, err := installBuildInstallObserver(model, gadgetMountDir, useEncryption)
	if err != nil {
		return err
	}

	options := gadgetInstall.Options{
		Mount:          true,
		EncryptionType: encryptionSupport,
	}

	validationConstraints := gadget.ValidationConstraints{
		EncryptedData: useEncryption,
	}

	gadgetInfo, err = gadget.ReadInfoAndValidate(gadgetMountDir, model, &validationConstraints)
	if err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}
	if err := gadget.ValidateContent(gadgetInfo, gadgetMountDir, kernelMountDir); err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}

	bootDevice := ""
	kernelSnapInfo := &gadgetInstall.KernelSnapInfo{
		Name:       kernelSnap.SnapName(),
		MountPoint: kernelMountDir,
		Revision:   kernelSnap.Revision,
		// Should be true always anyway
		IsCore: !model.Classic(),
	}
	switch model.Base() {
	case "core20", "core22", "core22-desktop":
		kernelSnapInfo.NeedsDriversTree = false
	default:
		kernelSnapInfo.NeedsDriversTree = true
	}

	installedSystem, err := gadgetInstallRun(model, gadgetMountDir, kernelSnapInfo, bootDevice, options, installObserver, timings.New(nil))
	if err != nil {
		return err
	}

	if trustedInstallObserver != nil {
		// We are required to call ObserveExistingTrustedRecoveryAssets on trusted observers
		if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
			return fmt.Errorf("cannot observe existing trusted recovery assets: %v", err)
		}
	}

	if useEncryption {
		if err := install.PrepareEncryptedSystemData(model, installedSystem.KeyForRole, trustedInstallObserver); err != nil {
			return err
		}
	}

	err = install.PrepareRunSystemData(model, gadgetMountDir, timings.New(nil))
	if err != nil {
		return err
	}

	bootWith := &boot.BootableSet{
		Base:                baseSnap,
		BasePath:            sysSnaps[snap.TypeBase].Path,
		Gadget:              gadgetSnap,
		GadgetPath:          sysSnaps[snap.TypeGadget].Path,
		Kernel:              kernelSnap,
		KernelPath:          sysSnaps[snap.TypeKernel].Path,
		UnpackedGadgetDir:   gadgetMountDir,
		RecoverySystemLabel: mst.recoverySystem,
	}

	if err := bootMakeRunnableStandaloneSystem(model, bootWith, trustedInstallObserver); err != nil {
		return err
	}

	dataMountOpts := &systemdMountOptions{
		Bind: true,
	}
	if err := doSystemdMount(boot.InstallUbuntuDataDir, boot.InitramfsDataDir, dataMountOpts); err != nil {
		return err
	}

	currentSeed, err := mst.LoadSeed(mst.recoverySystem)
	if err != nil {
		return err
	}
	preseedSeed, ok := currentSeed.(seed.PreseedCapable)
	if ok && preseedSeed.HasArtifact("preseed.tgz") {
		runMode := false
		if err := installApplyPreseededData(preseedSeed, boot.InitramfsWritableDir(model, runMode)); err != nil {
			return err
		}
	}

	if err := bootEnsureNextBootToRunMode(mst.recoverySystem); err != nil {
		return fmt.Errorf("failed to set system to run mode: %v\n", err)
	}

	mst.mode = "run"
	mst.recoverySystem = ""

	return nil
}

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with recover mode
	model, snaps, err := generateMountsCommonInstallRecoverStart(mst)
	if err != nil {
		return err
	}

	installAndRun, err := canInstallAndRunAtOnce(mst)
	if err != nil {
		return err
	}

	if installAndRun {
		if err := doInstall(mst, model, snaps); err != nil {
			return err
		}
		return nil
	} else {
		if err := generateMountsCommonInstallRecoverContinue(mst, model, snaps); err != nil {
			return err
		}

		// 3. final step: write modeenv to tmpfs data dir and disable cloud-init in
		//   install mode
		modeEnv, err := mst.EphemeralModeenvForModel(model, snaps)
		if err != nil {
			return err
		}
		isRunMode := false
		if err := modeEnv.WriteTo(boot.InitramfsWritableDir(model, isRunMode)); err != nil {
			return err
		}

		// done, no output, no error indicates to initramfs we are done with
		// mounting stuff
		return nil
	}
}

// copyNetworkConfig copies the network configuration to the target
// directory. This is used to copy the network configuration
// data from a real uc20 ubuntu-data partition into a ephemeral one.
func copyNetworkConfig(src, dst string) error {
	for _, globEx := range []string{
		// for network configuration setup by console-conf, etc.
		// TODO:UC20: we want some way to "try" or "verify" the network
		//            configuration or to only use known-to-be-good network
		//            configuration i.e. from ubuntu-save before installing it
		//            onto recover mode, because the network configuration could
		//            have been what was broken so we don't want to break
		//            network configuration for recover mode as well, but for
		//            now this is fine
		"system-data/etc/netplan/*",
		// etc/machine-id is part of what systemd-networkd uses to generate a
		// DHCP clientid (the other part being the interface name), so to have
		// the same IP addresses across run mode and recover mode, we need to
		// also copy the machine-id across
		"system-data/etc/machine-id",
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}
	return nil
}

// copyUbuntuDataMisc copies miscellaneous other files from the run mode system
// to the recover system such as:
//   - timesync clock to keep the same time setting in recover as in run mode
func copyUbuntuDataMisc(src, dst string) error {
	for _, globEx := range []string{
		// systemd's timesync clock file so that the time in recover mode moves
		// forward to what it was in run mode
		// NOTE: we don't sync back the time movement from recover mode to run
		// mode currently, unclear how/when we could do this, but recover mode
		// isn't meant to be long lasting and as such it's probably not a big
		// problem to "lose" the time spent in recover mode
		"system-data/var/lib/systemd/timesync/clock",
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}

	return nil
}

// copyUbuntuDataAuth copies the authentication files like
//   - extrausers passwd,shadow etc
//   - sshd host configuration
//   - user .ssh dir
//
// to the target directory. This is used to copy the authentication
// data from a real uc20 ubuntu-data partition into a ephemeral one.
func copyUbuntuDataAuth(src, dst string) error {
	for _, globEx := range []string{
		"system-data/var/lib/extrausers/*",
		"system-data/etc/ssh/*",
		"user-data/*/.ssh/*",
		// this ensures we get proper authentication to snapd from "snap"
		// commands in recover mode
		"user-data/*/.snap/auth.json",
		// this ensures we also get non-ssh enabled accounts copied
		"user-data/*/.profile",
		// so that users have proper perms, i.e. console-conf added users are
		// sudoers
		"system-data/etc/sudoers.d/*",
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}

	// ensure the user state is transferred as well
	srcState := filepath.Join(src, "system-data/var/lib/snapd/state.json")
	dstState := filepath.Join(dst, "system-data/var/lib/snapd/state.json")
	err := state.CopyState(srcState, dstState, []string{"auth.users", "auth.macaroon-key", "auth.last-id"})
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("cannot copy user state: %v", err)
	}

	return nil
}

// drop a marker file that disables console-conf
func disableConsoleConf(dst string) error {
	consoleConfCompleteFile := filepath.Join(dst, "system-data/var/lib/console-conf/complete")
	if err := os.MkdirAll(filepath.Dir(consoleConfCompleteFile), 0755); err != nil {
		return err
	}
	return os.WriteFile(consoleConfCompleteFile, nil, 0644)
}

// copySafeDefaultData will copy to the destination a "safe" set of data for
// a blank recover mode, i.e. one where we cannot copy authentication, etc. from
// the actual host ubuntu-data. Currently this is just a file to disable
// console-conf from running.
func copySafeDefaultData(dst string) error {
	return disableConsoleConf(dst)
}

func copyFromGlobHelper(src, dst, globEx string) error {
	matches, err := filepath.Glob(filepath.Join(src, globEx))
	if err != nil {
		return err
	}
	for _, p := range matches {
		comps := strings.Split(strings.TrimPrefix(p, src), "/")
		for i := range comps {
			part := filepath.Join(comps[0 : i+1]...)
			fi, err := os.Stat(filepath.Join(src, part))
			if err != nil {
				return err
			}
			if fi.IsDir() {
				if err := os.Mkdir(filepath.Join(dst, part), fi.Mode()); err != nil && !os.IsExist(err) {
					return err
				}
				st, ok := fi.Sys().(*syscall.Stat_t)
				if !ok {
					return fmt.Errorf("cannot get stat data: %v", err)
				}
				if err := os.Chown(filepath.Join(dst, part), int(st.Uid), int(st.Gid)); err != nil {
					return err
				}
			} else {
				if err := osutil.CopyFile(p, filepath.Join(dst, part), osutil.CopyFlagPreserveAll); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// states for partition state
const (
	// states for LocateState
	partitionFound      = "found"
	partitionNotFound   = "not-found"
	partitionErrFinding = "error-finding"
	// states for MountState
	partitionMounted          = "mounted"
	partitionErrMounting      = "error-mounting"
	partitionAbsentOptional   = "absent-but-optional"
	partitionMountedUntrusted = "mounted-untrusted"
	// states for UnlockState
	partitionUnlocked     = "unlocked"
	partitionErrUnlocking = "error-unlocking"
	// keys used to unlock for UnlockKey
	keyRun      = "run"
	keyFallback = "fallback"
	keyRecovery = "recovery"
)

// partitionState is the state of a partition after recover mode has completed
// for degraded mode.
type partitionState struct {
	// MountState is whether the partition was mounted successfully or not.
	MountState string `json:"mount-state,omitempty"`
	// MountLocation is where the partition was mounted.
	MountLocation string `json:"mount-location,omitempty"`
	// Device is what device the partition corresponds to. It can be the
	// physical block device if the partition is unencrypted or if it was not
	// successfully unlocked, or it can be a decrypted mapper device if the
	// partition was encrypted and successfully decrypted, or it can be the
	// empty string (or missing) if the partition was not found at all.
	Device string `json:"device,omitempty"`
	// FindState indicates whether the partition was found on the disk or not.
	FindState string `json:"find-state,omitempty"`
	// UnlockState was whether the partition was unlocked successfully or not.
	UnlockState string `json:"unlock-state,omitempty"`
	// UnlockKey was what key the partition was unlocked with, either "run",
	// "fallback" or "recovery".
	UnlockKey string `json:"unlock-key,omitempty"`

	// unexported internal fields for tracking the device, these are used during
	// state machine execution, and then combined into Device during finalize()
	// for simple representation to the consumer of degraded.json

	// fsDevice is what decrypted mapper device corresponds to the
	// partition, it can have the following states
	// - successfully decrypted => the decrypted mapper device
	// - unencrypted => the block device of the partition
	// - identified as decrypted, but failed to decrypt => empty string
	fsDevice string
	// partDevice is always the physical block device of the partition, in the
	// encrypted case this is the physical encrypted partition.
	partDevice string
}

type recoverDegradedState struct {
	// UbuntuData is the state of the ubuntu-data (or ubuntu-data-enc)
	// partition.
	UbuntuData partitionState `json:"ubuntu-data,omitempty"`
	// UbuntuBoot is the state of the ubuntu-boot partition.
	UbuntuBoot partitionState `json:"ubuntu-boot,omitempty"`
	// UbuntuSave is the state of the ubuntu-save (or ubuntu-save-enc)
	// partition.
	UbuntuSave partitionState `json:"ubuntu-save,omitempty"`
	// ErrorLog is the log of error messages encountered during recover mode
	// setting up degraded mode.
	ErrorLog []string `json:"error-log"`
}

func (r *recoverDegradedState) partition(part string) *partitionState {
	switch part {
	case "ubuntu-data":
		return &r.UbuntuData
	case "ubuntu-boot":
		return &r.UbuntuBoot
	case "ubuntu-save":
		return &r.UbuntuSave
	}
	panic(fmt.Sprintf("unknown partition %s", part))
}

func (r *recoverDegradedState) LogErrorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	r.ErrorLog = append(r.ErrorLog, msg)
	logger.Notice(msg)
}

func (r *recoverDegradedState) serializeTo(name string) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dirs.SnapBootstrapRunDir, 0755); err != nil {
		return err
	}

	// leave the information about degraded state at an ephemeral location
	return os.WriteFile(filepath.Join(dirs.SnapBootstrapRunDir, name), b, 0644)
}

// stateFunc is a function which executes a state action, returns the next
// function (for the next) state or nil if it is the final state.
type stateFunc func() (stateFunc, error)

// recoverModeStateMachine is a state machine implementing the logic for
// degraded recover mode.
// A full state diagram for the state machine can be found in
// /cmd/snap-bootstrap/degraded-recover-mode.svg in this repo.
type recoverModeStateMachine struct {
	// the current state is the one that is about to be executed
	current stateFunc

	// device model
	model *asserts.Model

	// the disk we have all our partitions on
	disk disks.Disk

	// when true, the fallback unlock paths will not be tried
	noFallback bool

	// TODO:UC20: for clarity turn this into into tristate:
	// unknown|encrypted|unencrypted
	isEncryptedDev bool

	// state for tracking what happens as we progress through degraded mode of
	// recovery
	degradedState *recoverDegradedState
}

func (m *recoverModeStateMachine) whichModel() (*asserts.Model, error) {
	return m.model, nil
}

// degraded returns whether a degraded recover mode state has fallen back from
// the typical operation to some sort of degraded mode.
func (m *recoverModeStateMachine) degraded() bool {
	r := m.degradedState

	if m.isEncryptedDev {
		// for encrypted devices, we need to have ubuntu-save mounted
		if r.UbuntuSave.MountState != partitionMounted {
			return true
		}

		// we also should have all the unlock keys as run keys
		if r.UbuntuData.UnlockKey != keyRun {
			return true
		}

		if r.UbuntuSave.UnlockKey != keyRun {
			return true
		}
	} else {
		// for unencrypted devices, ubuntu-save must either be mounted or
		// absent-but-optional
		if r.UbuntuSave.MountState != partitionMounted {
			if r.UbuntuSave.MountState != partitionAbsentOptional {
				return true
			}
		}
	}

	// ubuntu-boot and ubuntu-data should both be mounted
	if r.UbuntuBoot.MountState != partitionMounted {
		return true
	}
	if r.UbuntuData.MountState != partitionMounted {
		return true
	}

	// TODO: should we also check MountLocation too?

	// we should have nothing in the error log
	if len(r.ErrorLog) != 0 {
		return true
	}

	return false
}

func (m *recoverModeStateMachine) setFindState(partName, partUUID string, err error, optionalPartition bool) error {
	part := m.degradedState.partition(partName)
	if err != nil {
		if _, ok := err.(disks.PartitionNotFoundError); ok {
			// explicit error that the device was not found
			part.FindState = partitionNotFound
			if !optionalPartition {
				// partition is not optional, thus the error is relevant
				m.degradedState.LogErrorf("cannot find %v partition on disk %s", partName, m.disk.Dev())
			}
			return nil
		}
		// the error is not "not-found", so we have a real error
		part.FindState = partitionErrFinding
		m.degradedState.LogErrorf("error finding %v partition on disk %s: %v", partName, m.disk.Dev(), err)
		return nil
	}

	// device was found
	part.FindState = partitionFound
	dev := fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID)
	part.partDevice = dev
	part.fsDevice = dev
	return nil
}

func (m *recoverModeStateMachine) setMountState(part, where string, err error) error {
	if err != nil {
		m.degradedState.LogErrorf("cannot mount %v: %v", part, err)
		m.degradedState.partition(part).MountState = partitionErrMounting
		return nil
	}

	m.degradedState.partition(part).MountState = partitionMounted
	m.degradedState.partition(part).MountLocation = where

	return nil
}

func (m *recoverModeStateMachine) setUnlockStateWithRunKey(partName string, unlockRes secboot.UnlockResult, err error) error {
	part := m.degradedState.partition(partName)
	// save the device if we found it from secboot
	if unlockRes.PartDevice != "" {
		part.FindState = partitionFound
		part.partDevice = unlockRes.PartDevice
		part.fsDevice = unlockRes.FsDevice
	} else {
		part.FindState = partitionNotFound
	}
	if unlockRes.IsEncrypted {
		m.isEncryptedDev = true
	}

	if err != nil {
		// create different error message for encrypted vs unencrypted
		if unlockRes.IsEncrypted {
			// if we know the device is decrypted we must also always know at
			// least the partDevice (which is the encrypted block device)
			m.degradedState.LogErrorf("cannot unlock encrypted %s (device %s) with sealed run key: %v", partName, part.partDevice, err)
			part.UnlockState = partitionErrUnlocking
		} else {
			// TODO: we don't know if this is a plain not found or  a different error
			m.degradedState.LogErrorf("cannot locate %s partition for mounting host data: %v", partName, err)
		}

		return nil
	}

	if unlockRes.IsEncrypted {
		// unlocked successfully
		part.UnlockState = partitionUnlocked
		part.UnlockKey = keyRun
	}

	return nil
}

func (m *recoverModeStateMachine) setUnlockStateWithFallbackKey(partName string, unlockRes secboot.UnlockResult, err error) error {
	// first check the result and error for consistency; since we are using udev
	// there could be inconsistent results at different points in time

	// TODO: consider refactoring UnlockVolumeUsingSealedKeyIfEncrypted to not
	//       also find the partition on the disk, that should eliminate this
	//       consistency checking as we can code it such that we don't get these
	//       possible inconsistencies

	// do basic consistency checking on unlockRes to make sure the
	// result makes sense.
	if unlockRes.FsDevice != "" && err != nil {
		// This case should be impossible to enter, we can't
		// have a filesystem device but an error set
		return fmt.Errorf("internal error: inconsistent return values from UnlockVolumeUsingSealedKeyIfEncrypted for partition %s: %v", partName, err)
	}

	part := m.degradedState.partition(partName)
	// Also make sure that if we previously saw a partition device that we see
	// the same device again.
	if unlockRes.PartDevice != "" && part.partDevice != "" && unlockRes.PartDevice != part.partDevice {
		return fmt.Errorf("inconsistent partitions found for %s: previously found %s but now found %s", partName, part.partDevice, unlockRes.PartDevice)
	}

	// ensure consistency between encrypted state of the device/disk and what we
	// may have seen previously
	if m.isEncryptedDev && !unlockRes.IsEncrypted {
		// then we previously were able to positively identify an
		// ubuntu-data-enc but can't anymore, so we have inconsistent results
		// from inspecting the disk which is suspicious and we should fail
		return fmt.Errorf("inconsistent disk encryption status: previous access resulted in encrypted, but now is unencrypted from partition %s", partName)
	}

	// now actually process the result into the state
	if unlockRes.PartDevice != "" {
		part.FindState = partitionFound
		// Note that in some case this may be redundantly assigning the same
		// value to partDevice again.
		part.partDevice = unlockRes.PartDevice
		part.fsDevice = unlockRes.FsDevice
	}

	// There are a few cases where this could be the first time that we found a
	// decrypted device in the UnlockResult, but m.isEncryptedDev is still
	// false.
	// - The first case is if we couldn't find ubuntu-boot at all, in which case
	// we can't use the run object keys from there and instead need to directly
	// fallback to trying the fallback object keys from ubuntu-seed
	// - The second case is if we couldn't identify an ubuntu-data-enc or an
	// ubuntu-data partition at all, we still could have an ubuntu-save-enc
	// partition in which case we maybe could still have an encrypted disk that
	// needs unlocking with the fallback object keys from ubuntu-seed
	//
	// As such, if m.isEncryptedDev is false, but unlockRes.IsEncrypted is
	// true, then it is safe to assign m.isEncryptedDev to true.
	if !m.isEncryptedDev && unlockRes.IsEncrypted {
		m.isEncryptedDev = true
	}

	if err != nil {
		// create different error message for encrypted vs unencrypted
		if m.isEncryptedDev {
			m.degradedState.LogErrorf("cannot unlock encrypted %s partition with sealed fallback key: %v", partName, err)
			part.UnlockState = partitionErrUnlocking
		} else {
			// if we don't have an encrypted device and err != nil, then the
			// device must be not-found, see above checks

			// log an error the partition is mandatory
			m.degradedState.LogErrorf("cannot locate %s partition: %v", partName, err)
		}

		return nil
	}

	if m.isEncryptedDev {
		// unlocked successfully
		part.UnlockState = partitionUnlocked

		// figure out which key/method we used to unlock the partition
		switch unlockRes.UnlockMethod {
		case secboot.UnlockedWithSealedKey:
			part.UnlockKey = keyFallback
		case secboot.UnlockedWithRecoveryKey:
			part.UnlockKey = keyRecovery

			// TODO: should we fail with internal error for default case here?
		}
	}

	return nil
}

func newRecoverModeStateMachine(model *asserts.Model, disk disks.Disk, allowFallback bool) *recoverModeStateMachine {
	m := &recoverModeStateMachine{
		model: model,
		disk:  disk,
		degradedState: &recoverDegradedState{
			ErrorLog: []string{},
		},
		noFallback: !allowFallback,
	}
	// first step is to mount ubuntu-boot to check for run mode keys to unlock
	// ubuntu-data
	m.current = m.mountBoot
	return m
}

func (m *recoverModeStateMachine) execute() (finished bool, err error) {
	next, err := m.current()
	m.current = next
	finished = next == nil
	if finished && err == nil {
		if err := m.finalize(); err != nil {
			return true, err
		}
	}
	return finished, err
}

func (m *recoverModeStateMachine) finalize() error {
	// check soundness
	// the grade check makes sure that if data was mounted unencrypted
	// but the model is secured it will end up marked as untrusted
	isEncrypted := m.isEncryptedDev || m.model.StorageSafety() == asserts.StorageSafetyEncrypted
	part := m.degradedState.partition("ubuntu-data")
	if part.MountState == partitionMounted && isEncrypted {
		// check that save and data match
		// We want to avoid a chosen ubuntu-data
		// (e.g. activated with a recovery key) to get access
		// via its logins to the secrets in ubuntu-save (in
		// particular the policy update auth key)
		// TODO:UC20: we should try to be a bit more specific here in checking that
		//       data and save match, and not mark data as untrusted if we
		//       know that the real save is locked/protected (or doesn't exist
		//       in the case of bad corruption) because currently this code will
		//       mark data as untrusted, even if it was unlocked with the run
		//       object key and we failed to unlock ubuntu-save at all, which is
		//       undesirable. This effectively means that you need to have both
		//       ubuntu-data and ubuntu-save unlockable and have matching marker
		//       files in order to use the files from ubuntu-data to log-in,
		//       etc.
		trustData, _ := checkDataAndSavePairing(boot.InitramfsHostWritableDir(m.model))
		if !trustData {
			part.MountState = partitionMountedUntrusted
			m.degradedState.LogErrorf("cannot trust ubuntu-data, ubuntu-save and ubuntu-data are not marked as from the same install")
		}
	}

	// finally, combine the states of partDevice and fsDevice into the
	// exported Device field for marshalling
	// ubuntu-boot is easy - it will always be unencrypted so we just set
	// Device to partDevice
	m.degradedState.partition("ubuntu-boot").Device = m.degradedState.partition("ubuntu-boot").partDevice

	// for ubuntu-data and save, we need to actually look at the states
	for _, partName := range []string{"ubuntu-data", "ubuntu-save"} {
		part := m.degradedState.partition(partName)
		if part.fsDevice == "" {
			// then the device is encrypted, but we failed to decrypt it, so
			// set Device to the encrypted block device
			part.Device = part.partDevice
		} else {
			// all other cases, fsDevice is set to what we want to
			// export, either it is set to the decrypted mapper device in the
			// case it was successfully decrypted, or it is set to the encrypted
			// block device if we failed to decrypt it, or it was set to the
			// unencrypted block device if it was unencrypted
			part.Device = part.fsDevice
		}
	}

	return nil
}

func (m *recoverModeStateMachine) trustData() bool {
	return m.degradedState.partition("ubuntu-data").MountState == partitionMounted
}

// mountBoot is the first state to execute in the state machine, it can
// transition to the following states:
//   - if ubuntu-boot is mounted successfully, execute unlockDataRunKey
//   - if ubuntu-boot can't be mounted, execute unlockDataFallbackKey
//   - if we mounted the wrong ubuntu-boot (or otherwise can't verify which one we
//     mounted), return fatal error
func (m *recoverModeStateMachine) mountBoot() (stateFunc, error) {
	part := m.degradedState.partition("ubuntu-boot")
	// use the disk we mounted ubuntu-seed from as a reference to find
	// ubuntu-seed and mount it
	partUUID, findErr := m.disk.FindMatchingPartitionUUIDWithFsLabel("ubuntu-boot")
	const partitionMandatory = false
	if err := m.setFindState("ubuntu-boot", partUUID, findErr, partitionMandatory); err != nil {
		return nil, err
	}
	if part.FindState != partitionFound {
		// if we didn't find ubuntu-boot, we can't try to unlock data with the
		// run key, and should instead just jump straight to attempting to
		// unlock with the fallback key
		return m.unlockDataFallbackKey, nil
	}

	// should we fsck ubuntu-boot? probably yes because on some platforms
	// (u-boot for example) ubuntu-boot is vfat and it could have been unmounted
	// dirtily, and we need to fsck it to ensure it is mounted safely before
	// reading keys from it
	systemdOpts := &systemdMountOptions{
		NeedsFsck: true,
		Private:   true,
	}
	mountErr := doSystemdMount(part.fsDevice, boot.InitramfsUbuntuBootDir, systemdOpts)
	if err := m.setMountState("ubuntu-boot", boot.InitramfsUbuntuBootDir, mountErr); err != nil {
		return nil, err
	}
	if part.MountState == partitionErrMounting {
		// if we didn't mount data, then try to unlock data with the
		// fallback key
		return m.unlockDataFallbackKey, nil
	}

	// next step try to unlock data with run object
	return m.unlockDataRunKey, nil
}

// stateUnlockDataRunKey will try to unlock ubuntu-data with the normal run-mode
// key, and if it fails, progresses to the next state, which is either:
// - failed to unlock data, but we know it's an encrypted device -> try to unlock with fallback key
// - failed to find data at all -> try to unlock save
// - unlocked data with run key -> mount data
func (m *recoverModeStateMachine) unlockDataRunKey() (stateFunc, error) {
	runModeKey := device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir)
	unlockOpts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		// don't allow using the recovery key to unlock, we only try using the
		// recovery key after we first try the fallback object
		AllowRecoveryKey: false,
		WhichModel:       m.whichModel,
	}
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", runModeKey, unlockOpts)
	if err := m.setUnlockStateWithRunKey("ubuntu-data", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// we couldn't unlock ubuntu-data with the primary key, or we didn't
		// find it in the unencrypted case
		if unlockRes.IsEncrypted {
			// we know the device is encrypted, so the next state is to try
			// unlocking with the fallback key
			return m.unlockDataFallbackKey, nil
		}

		// if we didn't even find the device to the point where it would have
		// been identified as decrypted or unencrypted device, we could have
		// just entirely lost ubuntu-data-enc, and we could still have an
		// encrypted device, so instead try to unlock ubuntu-save with the
		// fallback key, the logic there can also handle an unencrypted ubuntu-save
		return m.unlockMaybeEncryptedAloneSaveFallbackKey, nil
	}

	// otherwise successfully unlocked it (or just found it if it was unencrypted)
	// so just mount it
	return m.mountData, nil
}

func (m *recoverModeStateMachine) unlockDataFallbackKey() (stateFunc, error) {
	if m.noFallback {
		return nil, fmt.Errorf("cannot unlock ubuntu-data (fallback disabled)")
	}

	// try to unlock data with the fallback key on ubuntu-seed, which must have
	// been mounted at this point
	unlockOpts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		// we want to allow using the recovery key if the fallback key fails as
		// using the fallback object is the last chance before we give up trying
		// to unlock data
		AllowRecoveryKey: true,
		WhichModel:       m.whichModel,
	}
	// TODO: this prompts for a recovery key
	// TODO: we should somehow customize the prompt to mention what key we need
	// the user to enter, and what we are unlocking (as currently the prompt
	// says "recovery key" and the partition UUID for what is being unlocked)
	dataFallbackKey := device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", dataFallbackKey, unlockOpts)
	if err := m.setUnlockStateWithFallbackKey("ubuntu-data", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// skip trying to mount data, since we did not unlock data we cannot
		// open save with with the run key, so try the fallback one
		return m.unlockEncryptedSaveFallbackKey, nil
	}

	// unlocked it, now go mount it
	return m.mountData, nil
}

func (m *recoverModeStateMachine) mountData() (stateFunc, error) {
	data := m.degradedState.partition("ubuntu-data")
	// don't do fsck on the data partition, it could be corrupted
	// however, data should always be mounted nosuid to prevent snaps from
	// extracting suid executables there and trying to circumvent the sandbox
	mountOpts := &systemdMountOptions{
		NoSuid:  true,
		Private: true,
	}
	mountErr := doSystemdMount(data.fsDevice, boot.InitramfsHostUbuntuDataDir, mountOpts)
	if err := m.setMountState("ubuntu-data", boot.InitramfsHostUbuntuDataDir, mountErr); err != nil {
		return nil, err
	}
	if m.isEncryptedDev {
		if mountErr == nil {
			// if we succeeded in mounting data and we are encrypted, the next step
			// is to unlock save with the run key from ubuntu-data
			return m.unlockEncryptedSaveRunKey, nil
		} else {
			// we are encrypted and we failed to mount data successfully, meaning we
			// don't have the bare key from ubuntu-data to use, and need to fall back
			// to the sealed key from ubuntu-seed
			return m.unlockEncryptedSaveFallbackKey, nil
		}
	}

	// the data is not encrypted, in which case the ubuntu-save, if it
	// exists, will be plain too
	return m.openUnencryptedSave, nil
}

func (m *recoverModeStateMachine) unlockEncryptedSaveRunKey() (stateFunc, error) {
	// to get to this state, we needed to have mounted ubuntu-data on host, so
	// if encrypted, we can try to read the run key from host ubuntu-data
	saveKey := device.SaveKeyUnder(dirs.SnapFDEDirUnder(boot.InitramfsHostWritableDir(m.model)))
	key, err := os.ReadFile(saveKey)
	if err != nil {
		// log the error and skip to trying the fallback key
		m.degradedState.LogErrorf("cannot access run ubuntu-save key: %v", err)
		return m.unlockEncryptedSaveFallbackKey, nil
	}

	unlockRes, unlockErr := secbootUnlockEncryptedVolumeUsingKey(m.disk, "ubuntu-save", key)
	if err := m.setUnlockStateWithRunKey("ubuntu-save", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// failed to unlock with run key, try fallback key
		return m.unlockEncryptedSaveFallbackKey, nil
	}

	// unlocked it properly, go mount it
	return m.mountSave, nil
}

func (m *recoverModeStateMachine) unlockMaybeEncryptedAloneSaveFallbackKey() (stateFunc, error) {
	// we can only get here by not finding ubuntu-data at all, meaning the
	// system can still be encrypted and have an encrypted ubuntu-save,
	// which we will determine now

	// first check whether there is an encrypted save
	_, findErr := m.disk.FindMatchingPartitionUUIDWithFsLabel(secboot.EncryptedPartitionName("ubuntu-save"))
	if findErr == nil {
		// well there is one, go try and unlock it
		return m.unlockEncryptedSaveFallbackKey, nil
	}
	// encrypted ubuntu-save does not exist, there may still be an
	// unencrypted one
	return m.openUnencryptedSave, nil
}

func (m *recoverModeStateMachine) openUnencryptedSave() (stateFunc, error) {
	// do we have ubuntu-save at all?
	partSave := m.degradedState.partition("ubuntu-save")
	const partitionOptional = true
	partUUID, findErr := m.disk.FindMatchingPartitionUUIDWithFsLabel("ubuntu-save")
	if err := m.setFindState("ubuntu-save", partUUID, findErr, partitionOptional); err != nil {
		return nil, err
	}
	if partSave.FindState == partitionFound {
		// we have ubuntu-save, go mount it
		return m.mountSave, nil
	}

	// unencrypted ubuntu-save was not found, try to log something in case
	// the early boot output can be collected for debugging purposes
	if uuid, err := m.disk.FindMatchingPartitionUUIDWithFsLabel(secboot.EncryptedPartitionName("ubuntu-save")); err == nil {
		// highly unlikely that encrypted save exists
		logger.Noticef("ignoring unexpected encrypted ubuntu-save with UUID %q", uuid)
	} else {
		logger.Noticef("ubuntu-save was not found")
	}

	// save is optional in an unencrypted system
	partSave.MountState = partitionAbsentOptional

	// we're done, nothing more to try
	return nil, nil
}

func (m *recoverModeStateMachine) unlockEncryptedSaveFallbackKey() (stateFunc, error) {
	// try to unlock save with the fallback key on ubuntu-seed, which must have
	// been mounted at this point

	if m.noFallback {
		return nil, fmt.Errorf("cannot unlock ubuntu-save (fallback disabled)")
	}

	unlockOpts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		// we want to allow using the recovery key if the fallback key fails as
		// using the fallback object is the last chance before we give up trying
		// to unlock save
		AllowRecoveryKey: true,
		WhichModel:       m.whichModel,
	}
	saveFallbackKey := device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
	// TODO: this prompts again for a recover key, but really this is the
	// reinstall key we will prompt for
	// TODO: we should somehow customize the prompt to mention what key we need
	// the user to enter, and what we are unlocking (as currently the prompt
	// says "recovery key" and the partition UUID for what is being unlocked)
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-save", saveFallbackKey, unlockOpts)
	if err := m.setUnlockStateWithFallbackKey("ubuntu-save", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// all done, nothing left to try and mount, mounting ubuntu-save is the
		// last step but we couldn't find or unlock it
		return nil, nil
	}
	// otherwise we unlocked it, so go mount it
	return m.mountSave, nil
}

func (m *recoverModeStateMachine) mountSave() (stateFunc, error) {
	save := m.degradedState.partition("ubuntu-save")
	// TODO: should we fsck ubuntu-save ?
	mountOpts := &systemdMountOptions{
		Private: true,
	}
	mountErr := doSystemdMount(save.fsDevice, boot.InitramfsUbuntuSaveDir, mountOpts)
	if err := m.setMountState("ubuntu-save", boot.InitramfsUbuntuSaveDir, mountErr); err != nil {
		return nil, err
	}
	// all done, nothing left to try and mount
	return nil, nil
}

func generateMountsModeRecover(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with install mode
	model, snaps, err := generateMountsCommonInstallRecover(mst)
	if err != nil {
		return err
	}

	// get the disk that we mounted the ubuntu-seed partition from as a
	// reference point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuSeedDir, nil)
	if err != nil {
		return err
	}

	// for most cases we allow the use of fallback to unlock/mount things
	allowFallback := true

	tryingCurrentSystem, err := boot.InitramfsIsTryingRecoverySystem(mst.recoverySystem)
	if err != nil {
		if boot.IsInconsistentRecoverySystemState(err) {
			// there is some try recovery system state in bootenv
			// but it is inconsistent, make sure we clear it and
			// return back to run mode

			// finalize reboots or panics
			logger.Noticef("try recovery system state is inconsistent: %v", err)
			finalizeTryRecoverySystemAndReboot(model, boot.TryRecoverySystemOutcomeInconsistent)
		}
		return err
	}
	if tryingCurrentSystem {
		// but in this case, use only the run keys
		allowFallback = false

		// make sure that if rebooted, the next boot goes into run mode
		if err := boot.EnsureNextBootToRunMode(""); err != nil {
			return err
		}
	}

	// 3. run the state machine logic for mounting partitions, this involves
	//    trying to unlock then mount ubuntu-data, and then unlocking and
	//    mounting ubuntu-save
	//    see the state* functions for details of what each step does and
	//    possible transition points

	machine, err := func() (machine *recoverModeStateMachine, err error) {
		// first state to execute is to unlock ubuntu-data with the run key
		machine = newRecoverModeStateMachine(model, disk, allowFallback)
		for {
			finished, err := machine.execute()
			// TODO: consider whether certain errors are fatal or not
			if err != nil {
				return nil, err
			}
			if finished {
				break
			}
		}

		return machine, nil
	}()
	if tryingCurrentSystem {
		// end of the line for a recovery system we are only trying out,
		// this branch always ends with a reboot (or a panic)
		var outcome boot.TryRecoverySystemOutcome
		if err == nil && !machine.degraded() {
			outcome = boot.TryRecoverySystemOutcomeSuccess
		} else {
			outcome = boot.TryRecoverySystemOutcomeFailure
			if err == nil {
				err = fmt.Errorf("in degraded state")
			}
			logger.Noticef("try recovery system %q failed: %v", mst.recoverySystem, err)
		}
		// finalize reboots or panics
		finalizeTryRecoverySystemAndReboot(model, outcome)
	}

	if err != nil {
		return err
	}

	// 3.1 write out degraded.json if we ended up falling back somewhere
	if machine.degraded() {
		if err := machine.degradedState.serializeTo("degraded.json"); err != nil {
			return err
		}
	}

	// 4. final step: copy the auth data and network config from
	//    the real ubuntu-data dir to the ephemeral ubuntu-data
	//    dir, write the modeenv to the tmpfs data, and disable
	//    cloud-init in recover mode

	// if we have the host location, then we were able to successfully mount
	// ubuntu-data, and as such we can proceed with copying files from there
	// onto the tmpfs
	// Proceed only if we trust ubuntu-data to be paired with ubuntu-save
	if machine.trustData() {
		// TODO: erroring here should fallback to copySafeDefaultData and
		// proceed on with degraded mode anyways
		if err := copyUbuntuDataAuth(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
			return err
		}
		if err := copyNetworkConfig(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
			return err
		}
		if err := copyUbuntuDataMisc(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
			return err
		}
	} else {
		// we don't have ubuntu-data host mountpoint, so we should setup safe
		// defaults for i.e. console-conf in the running image to block
		// attackers from accessing the system - just because we can't access
		// ubuntu-data doesn't mean that attackers wouldn't be able to if they
		// could login

		if err := copySafeDefaultData(boot.InitramfsDataDir); err != nil {
			return err
		}
	}

	modeEnv, err := mst.EphemeralModeenvForModel(model, snaps)
	if err != nil {
		return err
	}
	isRunMode := false
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir(model, isRunMode)); err != nil {
		return err
	}

	// finally we need to modify the bootenv to mark the system as successful,
	// this ensures that when you reboot from recover mode without doing
	// anything else, you are auto-transitioned back to run mode
	// TODO:UC20: as discussed unclear we need to pass the recovery system here
	if err := boot.EnsureNextBootToRunMode(mst.recoverySystem); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
}

func generateMountsModeFactoryReset(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with install mode
	model, snaps, err := generateMountsCommonInstallRecover(mst)
	if err != nil {
		return err
	}

	// get the disk that we mounted the ubuntu-seed partition from as a
	// reference point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuSeedDir, nil)
	if err != nil {
		return err
	}
	// step 3: find ubuntu-save, unlock and mount, note that factory-reset
	// mode only cares about ubuntu-save, as ubuntu-data and ubuntu-boot
	// will be wiped anyway so we do not even bother looking up those
	// partitions (which may be corrupted too, hence factory-reset was
	// invoked)
	machine, err := func() (machine *recoverModeStateMachine, err error) {
		allowFallback := true
		machine = newRecoverModeStateMachine(model, disk, allowFallback)
		// start from looking up encrypted ubuntu-save and unlocking with the fallback key
		machine.current = machine.unlockMaybeEncryptedAloneSaveFallbackKey
		for {
			finished, err := machine.execute()
			// TODO: consider whether certain errors are fatal or not
			if err != nil {
				return nil, err
			}
			if finished {
				break
			}
		}
		return machine, nil
	}()

	if err != nil {
		return err
	}

	if err := machine.degradedState.serializeTo("factory-reset-bootstrap.json"); err != nil {
		return err
	}

	// disable console-conf as it won't be needed
	if err := disableConsoleConf(boot.InitramfsDataDir); err != nil {
		return err
	}

	modeEnv, err := mst.EphemeralModeenvForModel(model, snaps)
	if err != nil {
		return err
	}
	isRunMode := false
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir(model, isRunMode)); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
}

// checkDataAndSavePairing make sure that ubuntu-data and ubuntu-save
// come from the same install by comparing secret markers in them
func checkDataAndSavePairing(rootdir string) (bool, error) {
	marker1, marker2, err := device.ReadEncryptionMarkers(dirs.SnapFDEDirUnder(rootdir), dirs.SnapFDEDirUnderSave(boot.InitramfsUbuntuSaveDir))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(marker1, marker2) == 1, nil
}

// waitFile waits for the given file/device-node/directory to appear.
var waitFile = func(path string, wait time.Duration, n int) error {
	for i := 0; i < n; i++ {
		if osutil.FileExists(path) {
			return nil
		}
		time.Sleep(wait)
	}

	return fmt.Errorf("no %v after waiting for %v", path, time.Duration(n)*wait)
}

// TODO: those have to be waited by udev instead
func waitForDevice(path string) error {
	if !osutil.FileExists(filepath.Join(dirs.GlobalRootDir, path)) {
		pollWait := 50 * time.Millisecond
		pollIterations := 1200
		logger.Noticef("waiting up to %v for %v to appear", time.Duration(pollIterations)*pollWait, path)
		if err := waitFile(filepath.Join(dirs.GlobalRootDir, path), pollWait, pollIterations); err != nil {
			return fmt.Errorf("cannot find device: %v", err)
		}
	}
	return nil
}

// Defined externally for faster unit tests
var pollWaitForLabel = 50 * time.Millisecond
var pollWaitForLabelIters = 1200

// TODO: those have to be waited by udev instead
func waitForCandidateByLabelPath(label string) (string, error) {
	logger.Noticef("waiting up to %v for label %v to appear",
		time.Duration(pollWaitForLabelIters)*pollWaitForLabel, label)
	var err error
	for i := 0; i < pollWaitForLabelIters; i++ {
		var candidate string
		// Ideally depending on the type of error we would return
		// immediately or try again, but that would complicate code more
		// than necessary and the extra wait will happen only when we
		// will fail to boot anyway. Note also that this code is
		// actually racy as we could get a not-best-possible-label (say,
		// we get "Ubuntu-boot" while actually an exact "ubuntu-boot"
		// label exists but the link has not been created yet): this is
		// not a fully solvable problem although waiting by udev will
		// help if the disk is present on boot.
		if candidate, err = disks.CandidateByLabelPath(label); err == nil {
			logger.Noticef("label %q found", candidate)
			return candidate, nil
		}
		time.Sleep(pollWaitForLabel)
	}

	// This is the last error from CandidateByLabelPath
	return "", err
}

func getNonUEFISystemDisk(fallbacklabel string) (string, error) {
	values, err := kcmdline.KeyValues("snapd_system_disk")
	if err != nil {
		return "", err
	}
	if value, ok := values["snapd_system_disk"]; ok {
		if err := waitForDevice(value); err != nil {
			return "", err
		}
		systemdDisk, err := disks.DiskFromDeviceName(value)
		if err != nil {
			systemdDiskDevicePath, errDevicePath := disks.DiskFromDevicePath(value)
			if errDevicePath != nil {
				return "", fmt.Errorf("%q can neither be used as a device nor as a block: %v; %v", value, errDevicePath, err)
			}
			systemdDisk = systemdDiskDevicePath
		}
		partition, err := systemdDisk.FindMatchingPartitionWithFsLabel(fallbacklabel)
		if err != nil {
			return "", err
		}
		return partition.KernelDeviceNode, nil
	}

	candidate, err := waitForCandidateByLabelPath(fallbacklabel)
	if err != nil {
		return "", err
	}

	return candidate, nil
}

// mountNonDataPartitionMatchingKernelDisk will select the partition to mount at
// dir, using the boot package function FindPartitionUUIDForBootedKernelDisk to
// determine what partition the booted kernel came from. If which disk the
// kernel came from cannot be determined, then it will fallback to mounting via
// the specified disk label.
func mountNonDataPartitionMatchingKernelDisk(dir, fallbacklabel string) error {
	partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
	var partSrc string
	if err == nil {
		// TODO: the by-partuuid is only available on gpt disks, on mbr we need
		//       to use by-uuid or by-id
		partSrc = filepath.Join("/dev/disk/by-partuuid", partuuid)
	} else {
		partSrc, err = getNonUEFISystemDisk(fallbacklabel)
		if err != nil {
			return err
		}
	}

	// The partition uuid is read from the EFI variables. At this point
	// the kernel may not have initialized the storage HW yet so poll
	// here.
	if err := waitForDevice(partSrc); err != nil {
		return err
	}

	opts := &systemdMountOptions{
		// always fsck the partition when we are mounting it, as this is the
		// first partition we will be mounting, we can't know if anything is
		// corrupted yet
		NeedsFsck: true,
		// don't need nosuid option here, since this function is only used
		// for ubuntu-boot and ubuntu-seed, never ubuntu-data
		Private: true,
	}
	return doSystemdMount(partSrc, dir, opts)
}

func generateMountsCommonInstallRecoverStart(mst *initramfsMountsState) (model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap, err error) {
	// 1. always ensure seed partition is mounted first before the others,
	//      since the seed partition is needed to mount the snap files there
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed"); err != nil {
		return nil, nil, err
	}

	// load model and verified essential snaps metadata
	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd, snap.TypeGadget}

	theSeed, err := mst.LoadSeed("")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot load seed: %v", err)
	}

	perf := timings.New(nil)
	if err := theSeed.LoadEssentialMeta(typs, perf); err != nil {
		return nil, nil, fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", typs, err)
	}

	model = theSeed.Model()
	essSnaps := theSeed.EssentialSnaps()

	// 2.1. measure model
	err = stampedAction(fmt.Sprintf("%s-model-measured", mst.recoverySystem), func() error {
		return secbootMeasureSnapModelWhenPossible(func() (*asserts.Model, error) {
			return model, nil
		})
	})
	if err != nil {
		return nil, nil, err
	}
	// verified model from the seed is now measured
	mst.SetVerifiedBootModel(model)

	// at this point on a system with TPM-based encryption
	// data can be open only if the measured model matches the actual
	// expected recovery model we sealed against.
	// TODO:UC20: on ARM systems and no TPM with encryption
	// we need other ways to make sure that the disk is opened
	// and we continue booting only for expected recovery models

	// 2.2. (auto) select recovery system and mount seed snaps
	// TODO:UC20: do we need more cross checks here?

	systemSnaps := make(map[snap.Type]*seed.Snap)

	for _, essentialSnap := range essSnaps {
		systemSnaps[essentialSnap.EssentialType] = essentialSnap
		dir := snapTypeToMountDir[essentialSnap.EssentialType]
		// TODO:UC20: we need to cross-check the kernel path with snapd_recovery_kernel used by grub
		if err := doSystemdMount(essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, dir), mountReadOnlyOptions); err != nil {
			return nil, nil, err
		}
	}

	return model, systemSnaps, nil
}

func generateMountsCommonInstallRecoverContinue(mst *initramfsMountsState, model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap) (err error) {
	// TODO:UC20: after we have the kernel and base snaps mounted, we should do
	//            the bind mounts from the kernel modules on top of the base
	//            mount and delete the corresponding systemd units from the
	//            initramfs layout

	// TODO:UC20: after the kernel and base snaps are mounted, we should setup
	//            writable here as well to take over from "the-modeenv" script
	//            in the initrd too

	// TODO:UC20: after the kernel and base snaps are mounted and writable is
	//            mounted, we should also implement writable-paths here too as
	//            writing it in Go instead of shellscript is desirable

	// 2.3. mount "ubuntu-data" on a tmpfs, and also mount with nosuid to prevent
	// snaps from being able to bypass the sandbox by creating suid root files
	// there and try to escape the sandbox
	mntOpts := &systemdMountOptions{
		Tmpfs:   true,
		NoSuid:  true,
		Private: true,
	}
	err = doSystemdMount("tmpfs", boot.InitramfsDataDir, mntOpts)
	if err != nil {
		return err
	}

	// finally get the gadget snap from the essential snaps and use it to
	// configure the ephemeral system
	// should only be one seed snap
	gadgetSnap := squashfs.New(sysSnaps[snap.TypeGadget].Path)

	isRunMode := false
	// we need to configure the ephemeral system with defaults and such using
	// from the seed gadget
	configOpts := &sysconfig.Options{
		// never allow cloud-init to run inside the ephemeral system, in the
		// install case we don't want it to ever run, and in the recover case
		// cloud-init will already have run in run mode, so things like network
		// config and users should already be setup and we will copy those
		// further down in the setup for recover mode
		AllowCloudInit: false,
		TargetRootDir:  boot.InitramfsWritableDir(model, isRunMode),
		GadgetSnap:     gadgetSnap,
	}
	if err := sysconfig.ConfigureTargetSystem(model, configOpts); err != nil {
		return err
	}

	return nil
}

func generateMountsCommonInstallRecover(mst *initramfsMountsState) (model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap, err error) {
	model, snaps, err := generateMountsCommonInstallRecoverStart(mst)
	if err != nil {
		return nil, nil, err
	}

	if err := generateMountsCommonInstallRecoverContinue(mst, model, snaps); err != nil {
		return nil, nil, err
	}

	return model, snaps, nil
}

func maybeMountSave(disk disks.Disk, rootdir string, encrypted bool, mountOpts *systemdMountOptions) (haveSave bool, err error) {
	var saveDevice string
	if encrypted {
		saveKey := device.SaveKeyUnder(dirs.SnapFDEDirUnder(rootdir))
		// if ubuntu-save exists and is encrypted, the key has been created during install
		if !osutil.FileExists(saveKey) {
			// ubuntu-data is encrypted, but we appear to be missing
			// a key to open ubuntu-save
			return false, fmt.Errorf("cannot find ubuntu-save encryption key at %v", saveKey)
		}
		// we have save.key, volume exists and is encrypted
		key, err := os.ReadFile(saveKey)
		if err != nil {
			return true, err
		}
		unlockRes, err := secbootUnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", key)
		if err != nil {
			return true, fmt.Errorf("cannot unlock ubuntu-save volume: %v", err)
		}
		saveDevice = unlockRes.FsDevice
	} else {
		partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel("ubuntu-save")
		if err != nil {
			if _, ok := err.(disks.PartitionNotFoundError); ok {
				// this is ok, ubuntu-save may not exist for
				// non-encrypted device
				return false, nil
			}
			return false, err
		}
		saveDevice = filepath.Join("/dev/disk/by-partuuid", partUUID)
	}
	if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, mountOpts); err != nil {
		return true, err
	}
	return true, nil
}

// XXX: workaround for the lack of model in CVM systems
type genericCVMModel struct{}

func (*genericCVMModel) Classic() bool {
	return true
}

func (*genericCVMModel) Grade() asserts.ModelGrade {
	return "signed"
}

func generateMountsModeRunCVM(mst *initramfsMountsState) error {
	// Mount ESP as UbuntuSeedDir which has UEFI label
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "UEFI"); err != nil {
		return err
	}

	// get the disk that we mounted the ESP from as a reference
	// point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuSeedDir, nil)
	if err != nil {
		return err
	}

	// Mount rootfs
	if err := secbootProvisionForCVM(boot.InitramfsUbuntuSeedDir); err != nil {
		return err
	}
	runModeCVMKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "cloudimg-rootfs.sealed-key")
	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		AllowRecoveryKey: true,
	}
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "cloudimg-rootfs", runModeCVMKey, opts)
	if err != nil {
		return err
	}
	fsckSystemdOpts := &systemdMountOptions{
		NeedsFsck: true,
		Ephemeral: true,
	}
	if err := doSystemdMount(unlockRes.FsDevice, boot.InitramfsDataDir, fsckSystemdOpts); err != nil {
		return err
	}

	// Unmount ESP because otherwise unmounting is racy and results in booted systems without ESP
	if err := doSystemdMount("", boot.InitramfsUbuntuSeedDir, &systemdMountOptions{Umount: true, Ephemeral: true}); err != nil {
		return err
	}

	// There is no real model on a CVM device but minimal model
	// information is required by the later code
	mst.SetVerifiedBootModel(&genericCVMModel{})

	return nil
}

func generateMountsModeRun(mst *initramfsMountsState) error {
	// 1. mount ubuntu-boot
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuBootDir, "ubuntu-boot"); err != nil {
		return err
	}

	// get the disk that we mounted the ubuntu-boot partition from as a
	// reference point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuBootDir, nil)
	if err != nil {
		return err
	}

	// 1.1. measure model
	err = stampedAction("run-model-measured", func() error {
		return secbootMeasureSnapModelWhenPossible(mst.UnverifiedBootModel)
	})
	if err != nil {
		return err
	}
	// XXX: I wonder if secbootMeasureSnapModelWhenPossible()
	// should return the model so that we don't need to run
	// mst.UnverifiedBootModel() again
	model, err := mst.UnverifiedBootModel()
	if err != nil {
		return err
	}
	isClassic := model.Classic()
	if model.Classic() {
		logger.Noticef("generating mounts for classic system, run mode")
	} else {
		logger.Noticef("generating mounts for Ubuntu Core system, run mode")
	}
	isRunMode := true

	// 2. mount ubuntu-seed (optional for classic)
	systemdOpts := &systemdMountOptions{
		NeedsFsck: true,
		Private:   true,
	}
	// use the disk we mounted ubuntu-boot from as a reference to find
	// ubuntu-seed and mount it
	hasSeedPart := true
	partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel("ubuntu-seed")
	if err != nil {
		if isClassic {
			// If there is no ubuntu-seed on classic, that's fine
			if _, ok := err.(disks.PartitionNotFoundError); !ok {
				return err
			}
			hasSeedPart = false
		} else {
			return err
		}
	}
	// fsck is safe to run on ubuntu-seed as per the manpage, it should not
	// meaningfully contribute to corruption if we fsck it every time we boot,
	// and it is important to fsck it because it is vfat and mounted writable
	// TODO:UC20: mount it as read-only here and remount as writable when we
	//            need it to be writable for i.e. transitioning to recover mode
	if partUUID != "" {
		if err := doSystemdMount(fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID),
			boot.InitramfsUbuntuSeedDir, systemdOpts); err != nil {
			return err
		}
	}

	// 2.1 Update bootloader variables now that boot/seed are mounted
	if err := boot.InitramfsRunModeUpdateBootloaderVars(); err != nil {
		return err
	}

	// at this point on a system with TPM-based encryption
	// data can be open only if the measured model matches the actual
	// run model.
	// TODO:UC20: on ARM systems and no TPM with encryption
	// we need other ways to make sure that the disk is opened
	// and we continue booting only for expected models

	// 3.1. mount Data
	runModeKey := device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir)
	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		AllowRecoveryKey: true,
		WhichModel:       mst.UnverifiedBootModel,
	}
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, opts)
	if err != nil {
		return err
	}

	// TODO: do we actually need fsck if we are mounting a mapper device?
	// probably not?
	dataMountOpts := &systemdMountOptions{
		NeedsFsck: true,
	}
	if !isClassic {
		// fsck and mount with nosuid to prevent snaps from being able to bypass
		// the sandbox by creating suid root files there and trying to escape the
		// sandbox
		dataMountOpts.NoSuid = true
		// Note that on classic the default is to allow mount propagation
		dataMountOpts.Private = true
	}
	if err := doSystemdMount(unlockRes.FsDevice, boot.InitramfsDataDir, dataMountOpts); err != nil {
		return err
	}
	isEncryptedDev := unlockRes.IsEncrypted

	// at this point data was opened so we can consider the model okay
	mst.SetVerifiedBootModel(model)
	rootfsDir := boot.InitramfsWritableDir(model, isRunMode)

	// 3.2. mount ubuntu-save (if present)
	haveSave, err := maybeMountSave(disk, rootfsDir, isEncryptedDev, systemdOpts)
	if err != nil {
		return err
	}

	if haveSave {
		if isEncryptedDev {
			// in run mode the path to open an encrypted save is for
			// data to be encrypted and the save key in it
			// to be successfully used. This already should stop
			// allowing to chose ubuntu-data to try to access
			// save. as safety boot also stops if the keys cannot
			// be locked.
			// for symmetry with recover code and extra paranoia
			// though also check that the markers match.
			paired, err := checkDataAndSavePairing(rootfsDir)
			if err != nil {
				return err
			}
			if !paired {
				return fmt.Errorf("cannot validate boot: ubuntu-save and ubuntu-data are not marked as from the same install")
			}
		}
	}

	// 4.2. read modeenv
	modeEnv, err := boot.ReadModeenv(rootfsDir)
	if err != nil {
		return err
	}

	// order in the list must not change as it determines the mount order
	typs := []snap.Type{snap.TypeGadget, snap.TypeKernel}
	if !isClassic {
		typs = append([]snap.Type{snap.TypeBase}, typs...)
	}

	// 4.2 choose base, gadget and kernel snaps (this includes updating
	//     modeenv if needed to try the base snap)
	mounts, err := boot.InitramfsRunModeSelectSnapsToMount(typs, modeEnv, rootfsDir)
	if err != nil {
		return err
	}

	// TODO:UC20: with grade > dangerous, verify the kernel snap hash against
	//            what we booted using the tpm log, this may need to be passed
	//            to the function above to make decisions there, or perhaps this
	//            code actually belongs in the bootloader implementation itself

	// 4.3 mount base (if UC), gadget and kernel snaps
	for _, typ := range typs {
		if sn, ok := mounts[typ]; ok {
			dir := snapTypeToMountDir[typ]
			snapPath := filepath.Join(dirs.SnapBlobDirUnder(rootfsDir), sn.Filename())
			if err := doSystemdMount(snapPath, filepath.Join(boot.InitramfsRunMntDir, dir), mountReadOnlyOptions); err != nil {
				return err
			}
		}
	}

	// 4.4 check if we expected a ubuntu-seed partition from the gadget data
	if isClassic {
		gadgetDir := filepath.Join(boot.InitramfsRunMntDir, snapTypeToMountDir[snap.TypeGadget])
		foundRole, err := gadget.HasRole(gadgetDir, []string{gadget.SystemSeed, gadget.SystemSeedNull})
		if err != nil {
			return err
		}
		seedDefinedInGadget := foundRole != ""
		if hasSeedPart && !seedDefinedInGadget {
			return fmt.Errorf("ubuntu-seed partition found but not defined in the gadget")
		}
		if !hasSeedPart && seedDefinedInGadget {
			return fmt.Errorf("ubuntu-seed partition not found but defined in the gadget (%s)", foundRole)
		}
	}

	// 4.5 mount snapd snap only on first boot
	if modeEnv.RecoverySystem != "" && !isClassic {
		// load the recovery system and generate mount for snapd
		theSeed, err := mst.LoadSeed(modeEnv.RecoverySystem)
		if err != nil {
			return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
		}
		perf := timings.New(nil)
		if err := theSeed.LoadEssentialMeta([]snap.Type{snap.TypeSnapd}, perf); err != nil {
			return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
		}
		essSnaps := theSeed.EssentialSnaps()
		if err := doSystemdMount(essSnaps[0].Path, filepath.Join(boot.InitramfsRunMntDir, "snapd"), mountReadOnlyOptions); err != nil {
			return fmt.Errorf("cannot mount snapd snap: %v", err)
		}
	}

	return nil
}

var tryRecoverySystemHealthCheck = func(model gadget.Model) error {
	// check that writable is accessible by checking whether the
	// state file exists
	if !osutil.FileExists(dirs.SnapStateFileUnder(boot.InitramfsHostWritableDir(model))) {
		return fmt.Errorf("host state file is not accessible")
	}
	return nil
}

func finalizeTryRecoverySystemAndReboot(model gadget.Model, outcome boot.TryRecoverySystemOutcome) (err error) {
	// from this point on, we must finish with a system reboot
	defer func() {
		if rebootErr := boot.InitramfsReboot(); rebootErr != nil {
			if err != nil {
				err = fmt.Errorf("%v (cannot reboot to run system: %v)", err, rebootErr)
			} else {
				err = fmt.Errorf("cannot reboot to run system: %v", rebootErr)
			}
		}
		// not reached, unless in tests
		panic(fmt.Errorf("finalize try recovery system did not reboot, last error: %v", err))
	}()

	if outcome == boot.TryRecoverySystemOutcomeSuccess {
		if err := tryRecoverySystemHealthCheck(model); err != nil {
			// health checks failed, the recovery system is considered
			// unsuccessful
			outcome = boot.TryRecoverySystemOutcomeFailure
			logger.Noticef("try recovery system health check failed: %v", err)
		}
	}

	// that's it, we've tried booting a new recovery system to this point,
	// whether things are looking good or bad we will reboot back to run
	// mode and update the boot variables accordingly
	if err := boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(outcome); err != nil {
		logger.Noticef("cannot update the try recovery system state: %v", err)
		return fmt.Errorf("cannot mark recovery system successful: %v", err)
	}
	return nil
}
