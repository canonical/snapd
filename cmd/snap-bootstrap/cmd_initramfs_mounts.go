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
	"io/fs"
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
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	fdeBackend "github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/systemd"

	// to set sysconfig.ApplyFilesystemOnlyDefaultsImpl
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/snapfile"
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
	fdeBackend.RunFDESetupHook = runFDESetupHook

	logger.Noticef("snap-bootstrap version %v starting", snapdtool.Version)

	return generateInitramfsMounts()
}

var (
	osutilIsMounted = osutil.IsMounted
	osGetenv        = os.Getenv

	snapTypeToMountDir = map[snap.Type]string{
		snap.TypeBase:   "base",
		snap.TypeGadget: "gadget",
		snap.TypeKernel: "kernel",
		snap.TypeSnapd:  "snapd",
	}

	secbootMeasureSnapSystemEpochWhenPossible     func() error
	secbootMeasureSnapModelWhenPossible           func(findModel func() (*asserts.Model, error)) error
	secbootUnlockVolumeUsingSealedKeyIfEncrypted  func(disk disks.Disk, name string, encryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error)
	secbootUnlockEncryptedVolumeUsingProtectorKey func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error)

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
	// Comes from the seed and it might be unasserted, set revision in that case
	if info.Revision.Unset() {
		info.Revision = snap.R(-1)
	}
	return info, nil
}

func readComponentInfo(seedComp *seed.Component, mntPt string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
	container := snapdir.New(mntPt)
	ci, err := snap.ReadComponentInfoFromContainer(container, snapInfo, csi)
	if err != nil {
		return nil, err
	}
	// Comes from the seed and it might be unasserted, set revision in that case
	if ci.Revision.Unset() {
		ci.Revision = snap.R(-1)
	}
	return ci, nil
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

func readSnapInfoFromSeed(seedSnap *seed.Snap) (*snap.Info, error) {
	snapf, err := snapfile.Open(seedSnap.Path)
	if err != nil {
		return nil, err
	}
	info, err := snap.ReadInfoFromSnapFile(snapf, seedSnap.SideInfo)
	if err != nil {
		return nil, err

	}

	// Comes from the seed and it might be unasserted, set revision in that case
	if info.Revision.Unset() {
		info.Revision = snap.R(-1)
	}
	return info, nil
}

func doInstall(mst *initramfsMountsState, model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap) error {
	kernelSnap, err := readSnapInfo(sysSnaps, snap.TypeKernel)
	if err != nil {
		return err
	}
	var baseSnap *snap.Info
	if createSysrootMount() {
		// On UC24+ the base is not mounted yet, peek into the file
		baseSnap, err = readSnapInfoFromSeed(sysSnaps[snap.TypeBase])
	} else {
		baseSnap, err = readSnapInfo(sysSnaps, snap.TypeBase)
	}
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
	useEncryption := (encryptionSupport != device.EncryptionTypeNone)

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

	// Get kernel-modules information to have them ready early on first boot

	kernCompsByName := make(map[string]*snap.Component)
	for _, c := range kernelSnap.Components {
		kernCompsByName[c.Name] = c
	}

	kernelSeed := sysSnaps[snap.TypeKernel]
	kernCompsMntPts := make(map[string]string)
	compSeedInfos := []install.ComponentSeedInfo{}
	for _, sc := range kernelSeed.Components {
		seedComp := sc
		comp, ok := kernCompsByName[seedComp.CompSideInfo.Component.ComponentName]
		if !ok {
			return fmt.Errorf("component %s in seed but not defined by snap!",
				seedComp.CompSideInfo.Component.ComponentName)
		}
		if comp.Type != snap.KernelModulesComponent {
			continue
		}

		// Mount ephemerally the kernel-modules components to read
		// their metadata and also to make them accessible if building
		// the drivers tree.
		mntPt := filepath.Join(filepath.Join(boot.InitramfsRunMntDir, "snap-content",
			seedComp.CompSideInfo.Component.String()))
		if err := doSystemdMount(seedComp.Path, mntPt, &systemdMountOptions{
			ReadOnly:  true,
			Private:   true,
			Ephemeral: true}); err != nil {
			return err
		}
		kernCompsMntPts[seedComp.CompSideInfo.Component.String()] = mntPt

		defer func() {
			stdout, stderr, err := osutil.RunSplitOutput("systemd-mount", "--umount", mntPt)
			if err != nil {
				logger.Noticef("cannot unmount component in %s: %v",
					mntPt, osutil.OutputErrCombine(stdout, stderr, err))
			}
		}()

		compInfo, err := readComponentInfo(&seedComp, mntPt, kernelSnap, &seedComp.CompSideInfo)
		if err != nil {
			return err
		}
		compSeedInfos = append(compSeedInfos, install.ComponentSeedInfo{
			Info: compInfo,
			Seed: &seedComp,
		})
	}

	currentSeed, err := mst.LoadSeed(mst.recoverySystem)
	if err != nil {
		return err
	}
	preseedSeed, ok := currentSeed.(seed.PreseedCapable)
	preseed := false
	if ok && preseedSeed.HasArtifact("preseed.tgz") {
		preseed = true
	}
	// Drivers tree will already be built if using the preseed tarball
	needsKernelSetup := kernel.NeedsKernelDriversTree(model) && !preseed

	isCore := !model.Classic()
	kernelBootInfo := install.BuildKernelBootInfo(
		kernelSnap, compSeedInfos, kernelMountDir, kernCompsMntPts,
		install.BuildKernelBootInfoOpts{IsCore: isCore, NeedsDriversTree: needsKernelSetup})

	bootDevice := ""
	installedSystem, err := gadgetInstallRun(model, gadgetMountDir, kernelBootInfo.KSnapInfo, bootDevice, options, installObserver, timings.New(nil))
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
		if err := install.PrepareEncryptedSystemData(model, installedSystem.BootstrappedContainerForRole, nil, trustedInstallObserver); err != nil {
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
		KernelMods:          kernelBootInfo.BootableKMods,
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

	// Now we can write the snapd mount unit (needed as this is the first boot)
	// It is debatable if we are in run mode or not as after installation
	// from initramfs we run as normal, but anyway this does not change
	// anything as this code is run only by UC.
	isRunMode := false
	rootfsDir := boot.InitramfsWritableDir(model, isRunMode)
	snapdSeed := sysSnaps[snap.TypeSnapd]
	if err := setupSeedSnapdSnap(rootfsDir, snapdSeed); err != nil {
		return err
	}

	if preseed {
		// Extract pre-seed tarball
		runMode := false
		if err := installApplyPreseededData(preseedSeed,
			boot.InitramfsWritableDir(model, runMode)); err != nil {
			return err
		}
	}

	// Create drivers tree mount units to make it available before switch root.
	// daemon-reload is not needed because it is done from initramfs later, this
	// happens because on UC /etc/fstab is changed and systemd's
	// initrd-parse-etc.service does the reload, as it detects entries with the
	// x-initrd.mount option.
	hasDriversTree, err := createKernelMounts(
		rootfsDir, kernelSnap.SnapName(), kernelSnap.Revision, !isCore)
	if err != nil {
		return err
	}

	if hasDriversTree {
		// Unmount the kernel snap mount, we keep it only for UC20/22
		stdout, stderr, err := osutil.RunSplitOutput("systemd-mount", "--umount", kernelMountDir)
		if err != nil {
			return osutil.OutputErrCombine(stdout, stderr, err)
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
		kernSnap := snaps[snap.TypeKernel]
		// seed is cached at this point
		theSeed, err := mst.LoadSeed("")
		if err != nil {
			return fmt.Errorf("internal error: cannot load seed: %v", err)
		}
		// Filter by mode, this is relevant only to get the
		// kernel-modules components that are used in run mode and
		// therefore need to be considered when installing from the
		// initramfs to have the modules available early on first boot.
		// TODO when running normal install or recover/factory-reset,
		// we would need also this if we want the modules to be
		// available early.
		kernSnap, err = theSeed.ModeSnap(kernSnap.SnapName(), "run")
		if err != nil {
			return err
		}
		snaps[snap.TypeKernel] = kernSnap

		if err := doInstall(mst, model, snaps); err != nil {
			return err
		}
		return nil
	} else {
		if err := generateMountsCommonInstallRecoverContinue(model, snaps); err != nil {
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
//
// The given srcRoot should point to the directory that contains the writable
// host system data. The given dstRoot should point to the directory that
// contains the writable system data for the ephemeral recovery system.
func copyNetworkConfig(srcRoot, dstRoot string) error {
	for _, globEx := range []string{
		// for network configuration setup by console-conf, etc.
		// TODO:UC20: we want some way to "try" or "verify" the network
		//            configuration or to only use known-to-be-good network
		//            configuration i.e. from ubuntu-save before installing it
		//            onto recover mode, because the network configuration could
		//            have been what was broken so we don't want to break
		//            network configuration for recover mode as well, but for
		//            now this is fine
		"etc/netplan/*",
		// etc/machine-id is part of what systemd-networkd uses to generate a
		// DHCP clientid (the other part being the interface name), so to have
		// the same IP addresses across run mode and recover mode, we need to
		// also copy the machine-id across
		"etc/machine-id",
	} {
		if err := copyFromGlobHelper(srcRoot, dstRoot, globEx); err != nil {
			return err
		}
	}
	return nil
}

// copyUbuntuDataMisc copies miscellaneous other files from the run mode system
// to the recover system such as:
//   - timesync clock to keep the same time setting in recover as in run mode
//
// The given srcRoot should point to the directory that contains the writable
// host system data. The given dstRoot should point to the directory that
// contains the writable system data for the ephemeral recovery system.
func copyUbuntuDataMisc(srcRoot, dstRoot string) error {
	for _, globEx := range []string{
		// systemd's timesync clock file so that the time in recover mode moves
		// forward to what it was in run mode
		// NOTE: we don't sync back the time movement from recover mode to run
		// mode currently, unclear how/when we could do this, but recover mode
		// isn't meant to be long lasting and as such it's probably not a big
		// problem to "lose" the time spent in recover mode
		"var/lib/systemd/timesync/clock",
	} {
		if err := copyFromGlobHelper(srcRoot, dstRoot, globEx); err != nil {
			return err
		}
	}

	return nil
}

// copyCoreUbuntuAuthData copies the authentication files like
//   - extrausers passwd,shadow etc
//   - sshd host configuration
//   - user .ssh dir
//
// to the target directory. This is used to copy the authentication
// data from a real uc20 ubuntu-data partition into a ephemeral one.
func copyCoreUbuntuAuthData(srcUbuntuData, destUbuntuData string) error {
	for _, globEx := range []string{
		"system-data/var/lib/extrausers/*",
		"system-data/etc/ssh/*",
		// so that users have proper perms, i.e. console-conf added users are
		// sudoers
		"system-data/etc/sudoers.d/*",
		"user-data/*/.ssh/*",
		// this ensures we get proper authentication to snapd from "snap"
		// commands in recover mode
		"user-data/*/.snap/auth.json",
		// this ensures we also get non-ssh enabled accounts copied
		"user-data/*/.profile",
	} {
		if err := copyFromGlobHelper(srcUbuntuData, destUbuntuData, globEx); err != nil {
			return err
		}
	}

	// ensure the user state is transferred as well
	srcState := filepath.Join(srcUbuntuData, "system-data/var/lib/snapd/state.json")
	dstState := filepath.Join(destUbuntuData, "system-data/var/lib/snapd/state.json")
	err := state.CopyState(srcState, dstState, []string{"auth.users", "auth.macaroon-key", "auth.last-id"})
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("cannot copy user state: %v", err)
	}

	return nil
}

// copyHybridUbuntuDataAuth copies the authentication files that are relevant on
// a hybrid system to the ubuntu data directory. Non-user specific files are
// copied to <destUbuntuData>/system-data. User specific files are copied to
// <destUbuntuData>/user-data.
func copyHybridUbuntuDataAuth(srcUbuntuData, destUbuntuData string) error {
	destSystemData := filepath.Join(destUbuntuData, "system-data")
	for _, globEx := range []string{
		"etc/ssh/*",
		"etc/sudoers.d/*",
		"root/.ssh/*",
	} {
		if err := copyFromGlobHelper(
			srcUbuntuData,
			destSystemData,
			globEx,
		); err != nil {
			return err
		}
	}

	destHomeData := filepath.Join(srcUbuntuData, "home")
	destUserData := filepath.Join(destUbuntuData, "user-data")
	for _, globEx := range []string{
		"*/.ssh/*",
		"*/.snap/auth.json",
	} {
		if err := copyFromGlobHelper(
			destHomeData,
			destUserData,
			globEx,
		); err != nil {
			return err
		}
	}

	// ensure the user state is transferred as well
	srcState := filepath.Join(srcUbuntuData, "var/lib/snapd/state.json")
	dstState := filepath.Join(destUbuntuData, "system-data/var/lib/snapd/state.json")
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

// partitionState is the state of a partition after recover mode has completed
// for degraded mode.
type partitionState struct {
	boot.PartitionState

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

type diskUnlockState struct {
	// UbuntuData is the state of the ubuntu-data (or ubuntu-data-enc)
	// partition.
	UbuntuData partitionState
	// UbuntuBoot is the state of the ubuntu-boot partition.
	UbuntuBoot partitionState
	// UbuntuSave is the state of the ubuntu-save (or ubuntu-save-enc)
	// partition.
	UbuntuSave partitionState

	ErrorLog []string
}

func (r *diskUnlockState) serializeTo(name string) error {
	exportState := &boot.DiskUnlockState{
		UbuntuData: r.UbuntuData.PartitionState,
		UbuntuBoot: r.UbuntuBoot.PartitionState,
		UbuntuSave: r.UbuntuSave.PartitionState,
		ErrorLog:   r.ErrorLog,
	}

	return exportState.WriteTo(name)
}

func (r *diskUnlockState) LogErrorf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	r.ErrorLog = append(r.ErrorLog, msg)
	logger.Notice(msg)
}

func (r *diskUnlockState) partition(part string) *partitionState {
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

	// boot mode (factory-reset or recover)
	mode string

	// the disk we have all our partitions on
	disk disks.Disk

	// when true, the fallback unlock paths will not be tried
	noFallback bool

	// TODO:UC20: for clarity turn this into into tristate:
	// unknown|encrypted|unencrypted
	isEncryptedDev bool

	// state for tracking what happens as we progress through degraded mode of
	// recovery
	degradedState *diskUnlockState
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
		if r.UbuntuSave.MountState != boot.PartitionMounted {
			return true
		}

		// we also should have all the unlock keys as run keys
		if r.UbuntuData.UnlockKey != boot.KeyRun {
			return true
		}

		if r.UbuntuSave.UnlockKey != boot.KeyRun {
			return true
		}
	} else {
		// for unencrypted devices, ubuntu-save must either be mounted or
		// absent-but-optional
		if r.UbuntuSave.MountState != boot.PartitionMounted {
			if r.UbuntuSave.MountState != boot.PartitionAbsentOptional {
				return true
			}
		}
	}

	// ubuntu-boot and ubuntu-data should both be mounted
	if r.UbuntuBoot.MountState != boot.PartitionMounted {
		return true
	}
	if r.UbuntuData.MountState != boot.PartitionMounted {
		return true
	}

	// TODO: should we also check MountLocation too?

	// we should have nothing in the error log
	if len(r.ErrorLog) != 0 {
		return true
	}

	return false
}

func (m *recoverModeStateMachine) diskOpts() *disks.Options {
	if m.isEncryptedDev {
		return &disks.Options{
			IsDecryptedDevice: true,
		}
	}
	return nil
}

func (m *recoverModeStateMachine) verifyMountPoint(dir, name string) error {
	matches, err := m.disk.MountPointIsFromDisk(dir, m.diskOpts())
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("cannot validate mount: %s mountpoint target %s is expected to be from disk %s but is not", name, dir, m.disk.Dev())
	}
	return nil
}

func (m *recoverModeStateMachine) setFindState(partName, partUUID string, err error, optionalPartition bool) error {
	part := m.degradedState.partition(partName)

	if err != nil {
		if _, ok := err.(disks.PartitionNotFoundError); ok {
			// explicit error that the device was not found
			part.FindState = boot.PartitionNotFound
			if !optionalPartition {
				// partition is not optional, thus the error is relevant
				m.degradedState.LogErrorf("cannot find %v partition on disk %s", partName, m.disk.Dev())
			}
			return nil
		}
		// the error is not "not-found", so we have a real error
		part.FindState = boot.PartitionErrFinding
		m.degradedState.LogErrorf("error finding %v partition on disk %s: %v", partName, m.disk.Dev(), err)
		return nil
	}

	// device was found
	part.FindState = boot.PartitionFound
	dev := fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID)
	part.partDevice = dev
	part.fsDevice = dev
	return nil
}

func (m *recoverModeStateMachine) setMountState(part, where string, err error) error {
	if err != nil {
		m.degradedState.LogErrorf("cannot mount %v: %v", part, err)
		m.degradedState.partition(part).MountState = boot.PartitionErrMounting
		return nil
	}

	m.degradedState.partition(part).MountState = boot.PartitionMounted
	m.degradedState.partition(part).MountLocation = where

	if err := m.verifyMountPoint(where, part); err != nil {
		m.degradedState.LogErrorf("cannot verify %s mount point at %v: %v", part, where, err)
		return err
	}
	return nil
}

func (m *recoverModeStateMachine) setUnlockStateWithRunKey(partName string, unlockRes secboot.UnlockResult, err error) {
	if unlockRes.IsEncrypted {
		m.isEncryptedDev = true
	}
	m.degradedState.setUnlockStateWithRunKey(partName, unlockRes, err)
}

func (d *diskUnlockState) setUnlockStateWithRunKey(partName string, unlockRes secboot.UnlockResult, err error) {
	part := d.partition(partName)
	// save the device if we found it from secboot
	if unlockRes.PartDevice != "" {
		part.FindState = boot.PartitionFound
		part.partDevice = unlockRes.PartDevice
		part.fsDevice = unlockRes.FsDevice
	} else {
		part.FindState = boot.PartitionNotFound
	}

	if err != nil {
		// create different error message for encrypted vs unencrypted
		if unlockRes.IsEncrypted {
			// if we know the device is decrypted we must also always know at
			// least the partDevice (which is the encrypted block device)
			d.LogErrorf("cannot unlock encrypted %s (device %s) with sealed run key: %v", partName, part.partDevice, err)
			part.UnlockState = boot.PartitionErrUnlocking
		} else {
			// TODO: we don't know if this is a plain not found or  a different error
			d.LogErrorf("cannot locate %s partition for mounting host data: %v", partName, err)
		}

		return
	}

	if unlockRes.IsEncrypted {
		// unlocked successfully
		part.UnlockState = boot.PartitionUnlocked

		switch unlockRes.UnlockMethod {
		case secboot.UnlockedWithSealedKey:
			part.UnlockKey = boot.KeyRun
		case secboot.UnlockedWithRecoveryKey:
			part.UnlockKey = boot.KeyRecovery
		case secboot.UnlockedWithKey:
			// This is the case when opening the save with the key file
			part.UnlockKey = boot.KeyRun
		default:
			panic(fmt.Errorf("Unexpected unlock method: %v", unlockRes.UnlockMethod))
		}
	}
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
		part.FindState = boot.PartitionFound
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
			part.UnlockState = boot.PartitionErrUnlocking
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
		part.UnlockState = boot.PartitionUnlocked

		// figure out which key/method we used to unlock the partition
		switch unlockRes.UnlockMethod {
		case secboot.UnlockedWithSealedKey:
			part.UnlockKey = boot.KeyFallback
		case secboot.UnlockedWithRecoveryKey:
			part.UnlockKey = boot.KeyRecovery

			// TODO: should we fail with internal error for default case here?
		}
	}

	return nil
}

func newRecoverModeStateMachine(model *asserts.Model, bootMode string, disk disks.Disk, allowFallback bool) *recoverModeStateMachine {
	m := &recoverModeStateMachine{
		model: model,
		mode:  bootMode,
		disk:  disk,
		degradedState: &diskUnlockState{
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
	if part.MountState == boot.PartitionMounted && isEncrypted {
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
			part.MountState = boot.PartitionMountedUntrusted
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
	return m.degradedState.partition("ubuntu-data").MountState == boot.PartitionMounted
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
	if part.FindState != boot.PartitionFound {
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
	if part.MountState == boot.PartitionErrMounting {
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
		BootMode:         m.mode,
	}
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", runModeKey, unlockOpts)
	m.setUnlockStateWithRunKey("ubuntu-data", unlockRes, unlockErr)
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
		BootMode:         m.mode,
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

	unlockRes, unlockErr := secbootUnlockEncryptedVolumeUsingProtectorKey(m.disk, "ubuntu-save", key)
	m.setUnlockStateWithRunKey("ubuntu-save", unlockRes, unlockErr)
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
	if partSave.FindState == boot.PartitionFound {
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
	partSave.MountState = boot.PartitionAbsentOptional

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
		BootMode:         m.mode,
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
		NoDev:   true,
		NoSuid:  true,
		NoExec:  true,
	}
	mountErr := doSystemdMount(save.fsDevice, boot.InitramfsUbuntuSaveDir, mountOpts)
	if err := m.setMountState("ubuntu-save", boot.InitramfsUbuntuSaveDir, mountErr); err != nil {
		return nil, err
	}
	// all done, nothing left to try and mount
	return nil, nil
}

func (m *recoverModeStateMachine) writeRecoverUnlockState() error {
	// write out degraded.json if we ended up falling back somewhere
	if m.degraded() {
		if err := m.degradedState.serializeTo(boot.DegradedStateFileName); err != nil {
			return err
		}
	}

	// we always output unlocked.json
	return m.degradedState.serializeTo(boot.UnlockedStateFileName)
}

func (m *recoverModeStateMachine) writeFactoryResetUnlockState() error {
	if err := m.degradedState.serializeTo(boot.FactoryResetStateFileName); err != nil {
		return err
	}

	return m.degradedState.serializeTo(boot.UnlockedStateFileName)
}

func generateMountsModeRecover(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with install mode
	model, snaps, err := generateMountsRecoverOrFactoryReset(mst)
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
		machine = newRecoverModeStateMachine(model, "recover", disk, allowFallback)
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

	// 3.1 write out unlock states (unlocked.json, and eventually degraded.json)
	if err := machine.writeRecoverUnlockState(); err != nil {
		return err
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
		// on hybrid systems, we take special care to import the root user and
		// users from the "admin" and "sudo" groups into the ephemeral system.
		// this is our best-effort for allowing an owner of a hybrid system to
		// login to the created recovery system.
		hybrid := model.Classic() && model.KernelSnap() != nil

		hostSystemData := boot.InitramfsHostWritableDir(model)
		recoverySystemData := boot.InitramfsWritableDir(model, false)
		if hybrid {
			// TODO: eventually, the base will be mounted directly on /sysroot.
			// this will need to change once that happens.
			if err := importHybridUserData(
				hostSystemData,
				filepath.Join(boot.InitramfsRunMntDir, "base"),
			); err != nil {
				return err
			}

			if err := copyHybridUbuntuDataAuth(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
				return err
			}
		} else {
			// TODO: erroring here should fallback to copySafeDefaultData and
			// proceed on with degraded mode anyways
			if err := copyCoreUbuntuAuthData(
				boot.InitramfsHostUbuntuDataDir,
				boot.InitramfsDataDir,
			); err != nil {
				return err
			}
		}
		if err := copyNetworkConfig(hostSystemData, recoverySystemData); err != nil {
			return err
		}
		if err := copyUbuntuDataMisc(hostSystemData, recoverySystemData); err != nil {
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
	model, snaps, err := generateMountsRecoverOrFactoryReset(mst)
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
		machine = newRecoverModeStateMachine(model, "factory-reset", disk, allowFallback)
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

	if err := machine.writeFactoryResetUnlockState(); err != nil {
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

// mountNonDataPartitionMatchingKernelDisk will select the partition
// to mount at dir using the boot package function
// FindPartitionUUIDForBootedKernelDisk to determine what partition
// the booted kernel came from.
//
// If "snap-bootstrap scan-disk" was run as part of udev it will
// restrict the search of the partition from the boot disk it found.
//
// If "snap-bootstrap scan-disk" is not in use (legacy case),
// it will look for any partition that matches the boot.
//
// If which disk the kernel came from cannot be determined, then it
// will fallback to mounting via the specified disk label. If
// "snap-bootstrap scan-disk" was used, it will restrict the search to
// the boot disk.
func mountNonDataPartitionMatchingKernelDisk(dir, fallbacklabel string, opts *systemdMountOptions) error {
	var partSrc string

	if osutil.FileExists(filepath.Join(dirs.GlobalRootDir, "/dev/disk/snapd/disk")) {
		disk, err := disks.DiskFromDeviceName("/dev/disk/snapd/disk")
		if err != nil {
			return err
		}
		partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
		if err == nil {
			partition, err := disk.FindMatchingPartitionWithPartUUID(partuuid)
			if err != nil {
				return err
			}
			partSrc = partition.KernelDeviceNode
		} else {
			partition, err := disk.FindMatchingPartitionWithFsLabel(fallbacklabel)
			if err != nil {
				return err
			}
			partSrc = partition.KernelDeviceNode
		}
	} else {
		partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
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
	}
	return doSystemdMount(partSrc, dir, opts)
}

func createSysrootMount() bool {
	// This env var is set by snap-initramfs-mounts.service for 24+ initramfs. We
	// prefer this to checking the model so 24+ kernels can run with models using
	// older bases. Although this situation is not really supported as the
	// initramfs systemd bits would not match those in the base, we allow it as
	// it has been something done in the past and updates could break those
	// systems.
	isCore24plus := osGetenv("CORE24_PLUS_INITRAMFS")
	return isCore24plus == "1" || isCore24plus == "true"
}

func generateMountsCommonInstallRecoverStart(mst *initramfsMountsState) (model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap, err error) {
	seedMountOpts := &systemdMountOptions{
		// always fsck the partition when we are mounting it, as this is the
		// first partition we will be mounting, we can't know if anything is
		// corrupted yet
		NeedsFsck: true,
		Private:   true,
		NoSuid:    true,
		NoDev:     true,
		NoExec:    true,
	}

	// 1. always ensure seed partition is mounted first before the others,
	//      since the seed partition is needed to mount the snap files there
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed", seedMountOpts); err != nil {
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
		if essentialSnap.EssentialType == snap.TypeBase && createSysrootMount() {
			// Create unit to mount directly to /sysroot. We restrict
			// this to UC24+ for the moment, until we backport necessary
			// changes to the UC20/22 initramfs. Note that a transient
			// unit is not used as it tries to be restarted after the
			// switch root, and fails.
			what := essentialSnap.Path
			if err := writeSysrootMountUnit(what, "squashfs"); err != nil {
				return nil, nil, fmt.Errorf(
					"cannot write sysroot.mount (what: %s): %v", what, err)
			}
			// Do a daemon reload so systemd knows about the new sysroot mount unit
			// (populate-writable.service depends on sysroot.mount, we need to make
			// sure systemd knows this unit before snap-initramfs-mounts.service
			// finishes)
			sysd := systemd.New(systemd.SystemMode, nil)
			if err := sysd.DaemonReload(); err != nil {
				return nil, nil, err
			}
			// We need to restart initrd-root-fs.target so its dependencies are
			// re-calculated considering the new sysroot.mount unit. See
			// https://github.com/systemd/systemd/issues/23034 on why this is
			// needed.
			if err := sysd.StartNoBlock([]string{"initrd-root-fs.target"}); err != nil {
				return nil, nil, err
			}
			if model.Classic() && model.KernelSnap() != nil {
				// Mount ephemerally for recover mode to gain access to /etc data
				dir := snapTypeToMountDir[essentialSnap.EssentialType]
				if err := doSystemdMount(essentialSnap.Path,
					filepath.Join(boot.InitramfsRunMntDir, dir),
					&systemdMountOptions{
						Ephemeral: true,
						ReadOnly:  true,
						Private:   true,
					}); err != nil {
					return nil, nil, err
				}
			}
		} else if essentialSnap.EssentialType == snap.TypeSnapd {
			// We write later a unit for this one, when the data
			// partition is mounted
			continue
		} else {
			dir := snapTypeToMountDir[essentialSnap.EssentialType]
			// TODO:UC20: we need to cross-check the kernel path
			// with snapd_recovery_kernel used by grub
			if err := doSystemdMount(essentialSnap.Path,
				filepath.Join(boot.InitramfsRunMntDir, dir),
				mountReadOnlyOptions); err != nil {
				return nil, nil, err
			}
		}
	}

	return model, systemSnaps, nil
}

func generateMountsCommonInstallRecoverContinue(model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap) (err error) {
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

	// Now we can write the snapd mount unit (needed as this is the first boot)
	isRunMode := false
	rootfsDir := boot.InitramfsWritableDir(model, isRunMode)
	snapdSeed := sysSnaps[snap.TypeSnapd]
	if err := setupSeedSnapdSnap(rootfsDir, snapdSeed); err != nil {
		return err
	}

	// finally get the gadget snap from the essential snaps and use it to
	// configure the ephemeral system
	// should only be one seed snap
	gadgetSnap := squashfs.New(sysSnaps[snap.TypeGadget].Path)

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

func generateMountsRecoverOrFactoryReset(mst *initramfsMountsState) (model *asserts.Model, sysSnaps map[snap.Type]*seed.Snap, err error) {
	model, snaps, err := generateMountsCommonInstallRecoverStart(mst)
	if err != nil {
		return nil, nil, err
	}

	if err := generateMountsCommonInstallRecoverContinue(model, snaps); err != nil {
		return nil, nil, err
	}

	return model, snaps, nil
}

func maybeMountSave(disk disks.Disk, rootdir string, encrypted bool, mountOpts *systemdMountOptions) (haveSave bool, unlockRes secboot.UnlockResult, err error) {
	var saveDevice string
	if encrypted {
		saveKey := device.SaveKeyUnder(dirs.SnapFDEDirUnder(rootdir))
		// if ubuntu-save exists and is encrypted, the key has been created during install
		if !osutil.FileExists(saveKey) {
			// ubuntu-data is encrypted, but we appear to be missing
			// a key to open ubuntu-save
			return false, unlockRes, fmt.Errorf("cannot find ubuntu-save encryption key at %v", saveKey)
		}
		// we have save.key, volume exists and is encrypted
		key, err := os.ReadFile(saveKey)
		if err != nil {
			return true, unlockRes, err
		}
		unlockRes, err = secbootUnlockEncryptedVolumeUsingProtectorKey(disk, "ubuntu-save", key)
		if err != nil {
			return true, unlockRes, fmt.Errorf("cannot unlock ubuntu-save volume: %v", err)
		}
		saveDevice = unlockRes.FsDevice
	} else {
		partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel("ubuntu-save")
		if err != nil {
			if _, ok := err.(disks.PartitionNotFoundError); ok {
				// this is ok, ubuntu-save may not exist for
				// non-encrypted device
				return false, unlockRes, nil
			}
			return false, unlockRes, err
		}
		saveDevice = filepath.Join("/dev/disk/by-partuuid", partUUID)
	}
	if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, mountOpts); err != nil {
		return true, unlockRes, err
	}
	return true, unlockRes, nil
}

func createKernelMounts(runWritableDataDir, kernelName string, rev snap.Revision, isClassic bool) (bool, error) {
	driversStandardDir := kernel.DriversTreeDir(runWritableDataDir, kernelName, rev)
	// On UC first boot the drivers dir is initially under
	// _writable_defaults, so we need to check that directory too. But the
	// mount happens after handle-writable-paths has run, so the units
	// mounting /lib/{modules,firmware} can use driversStandardDir always.
	driversFirstBootDir := kernel.DriversTreeDir(
		filepath.Join(runWritableDataDir, "_writable_defaults"), kernelName, rev)
	var driversDir string
	switch {
	case osutil.IsDirectory(driversStandardDir):
		driversDir = driversStandardDir
	case osutil.IsDirectory(driversFirstBootDir):
		driversDir = driversFirstBootDir
	default:
		logger.Noticef("no drivers tree at %s", driversStandardDir)
		return false, nil
	}
	logger.Noticef("drivers tree found in %s", driversDir)

	// 1. Mount unit for the kernel snap
	cpi := snap.MinimalSnapContainerPlaceInfo(kernelName, rev)
	squashfsPath := filepath.Join(runWritableDataDir, dirs.StripRootDir(cpi.MountFile()))
	// snapRoot is where we will find the /snap directory where
	// snaps/components will be mounted
	snapRoot := filepath.Join("sysroot", "writable", "system-data")
	if isClassic {
		snapRoot = "sysroot"
	}
	where := filepath.Join(dirs.GlobalRootDir, snapRoot, dirs.StripRootDir(cpi.MountDir()))
	if err := writeInitramfsMountUnit(squashfsPath, where, squashfsUnit); err != nil {
		return false, err
	}

	// 2. Mount units for kernel-modules components
	if err := createKernelModulesMountUnits(
		runWritableDataDir, snapRoot, driversDir, kernelName); err != nil {
		return false, err
	}

	// 3. Mount units for /lib/{modules,firmware}
	for _, subDir := range []string{"modules", "firmware"} {
		what := filepath.Join(driversStandardDir, "lib", subDir)
		where := filepath.Join(dirs.GlobalRootDir, "sysroot", "usr", "lib", subDir)
		if err := writeInitramfsMountUnit(what, where, bindUnit); err != nil {
			return false, fmt.Errorf("while creating mount for %s in %s: %v",
				what, where, err)
		}
	}

	return true, nil
}

func createKernelModulesMountUnits(writableRootDir, snapRoot, driversDir, kernelName string) error {
	// Look for symlinks to kernel components. We care only about links to
	// content in the squashfs, links to $SNAP_DATA will just work as
	// /var/snap will be present before switch root.

	// First in modules (we might not have a kernel version subdir if there
	// are no kernel modules).
	kversion, kver := kernel.KernelVersionFromModulesDir(filepath.Join(driversDir, "lib"))
	compSet := map[snap.ComponentSideInfo]bool{}
	if kver == nil {
		modUpdatesDir := filepath.Join(driversDir, "lib", "modules", kversion, "updates")
		if err := getCompsFromSymlinks(modUpdatesDir, kernelName, compSet); err != nil {
			return err
		}
	}
	// Then look in firmware
	fwUpdatesDir := filepath.Join(driversDir, "lib", "firmware", "updates")
	if err := getCompsFromSymlinks(fwUpdatesDir, kernelName, compSet); err != nil {
		return err
	}

	// now create the component units
	for comp := range compSet {
		cpi := snap.MinimalComponentContainerPlaceInfo(
			comp.Component.ComponentName, comp.Revision, kernelName)
		squashfsPath := filepath.Join(writableRootDir, dirs.StripRootDir(cpi.MountFile()))
		where := filepath.Join(dirs.GlobalRootDir, snapRoot, dirs.StripRootDir(cpi.MountDir()))
		if err := writeInitramfsMountUnit(squashfsPath, where, squashfsUnit); err != nil {
			return err
		}
	}

	return nil
}

func getCompsFromSymlinks(symLinksDir, kernelName string, compSet map[snap.ComponentSideInfo]bool) error {
	entries, err := os.ReadDir(symLinksDir)
	if err != nil {
		// No updates folder, so there are no kernel-modules comps installed
		return nil
	}

	for _, node := range entries {
		if node.Type() != fs.ModeSymlink {
			continue
		}
		// Note that symlinks in drivers tree are absolute
		dest, err := os.Readlink(filepath.Join(symLinksDir, node.Name()))
		if err != nil {
			return err
		}

		// find out component name from symlink
		prefix := filepath.Join(snap.ComponentsBaseDir(kernelName), "mnt")
		subdir := strings.TrimPrefix(dest, prefix+string(os.PathSeparator))
		if subdir == dest {
			// Possibly points to $SNAP_DATA instead of to $SNAP,
			// or is a relative symlink to some fw file in the
			// component.
			continue
		}
		dirs := strings.Split(subdir, string(os.PathSeparator))
		// dirs should still have as a minimum 4 elements
		// <comp_name>/<comp_rev>/{modules/<kversion>,firmware/<filename>}
		if len(dirs) < 4 {
			logger.Noticef("warning: %s seems to be badly formed", dest)
			continue
		}
		rev, err := snap.ParseRevision(dirs[1])
		if err != nil {
			logger.Noticef("warning: wrong revision in symlink %s: %v", dest, err)
			continue
		}
		csi := snap.NewComponentSideInfo(naming.NewComponentRef(kernelName, dirs[0]), rev)
		compSet[*csi] = true
	}

	return nil
}

func recalculateRootfsTarget() error {
	// Do a daemon reload so systemd knows about the new sysroot mount unit
	// (populate-writable.service depends on sysroot.mount, we need to make
	// sure systemd knows this unit before snap-initramfs-mounts.service
	// finishes) and about the drivers tree mounts (relevant on hybrid).
	sysd := systemd.New(systemd.SystemMode, nil)
	if err := sysd.DaemonReload(); err != nil {
		return err
	}
	// We need to restart initrd-root-fs.target so its dependencies are
	// re-calculated considering the new sysroot.mount unit. See
	// https://github.com/systemd/systemd/issues/23034 on why this is
	// needed.
	return sysd.StartNoBlock([]string{"initrd-root-fs.target"})
}

func generateMountsModeRun(mst *initramfsMountsState) error {
	bootMountOpts := &systemdMountOptions{
		// always fsck the partition when we are mounting it, as this is the
		// first partition we will be mounting, we can't know if anything is
		// corrupted yet
		NeedsFsck: true,
		Private:   true,
	}

	// 1. mount ubuntu-boot
	if err := mountNonDataPartitionMatchingKernelDisk(boot.InitramfsUbuntuBootDir, "ubuntu-boot", bootMountOpts); err != nil {
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
	seedMountOpts := &systemdMountOptions{
		NeedsFsck: true,
		Private:   true,
		NoSuid:    true,
		NoDev:     true,
		NoExec:    true,
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
			boot.InitramfsUbuntuSeedDir, seedMountOpts); err != nil {
			return err
		}
	}

	// 2.1 Update bootloader variables now that boot/seed are mounted
	if err := boot.InitramfsRunModeUpdateBootloaderVars(); err != nil {
		return err
	}

	diskState := &diskUnlockState{}

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
		BootMode:         mst.mode,
	}
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, opts)
	if err != nil {
		return err
	}

	diskState.setUnlockStateWithRunKey("ubuntu-data", unlockRes, nil)

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
	saveMountOpts := &systemdMountOptions{
		NeedsFsck: true,
		Private:   true,
		NoDev:     true,
		NoSuid:    true,
		NoExec:    true,
	}
	haveSave, saveUnlockRes, err := maybeMountSave(disk, rootfsDir, isEncryptedDev, saveMountOpts)
	if err != nil {
		return err
	}

	// 4.1 verify that ubuntu-data comes from where we expect it to
	diskOpts := &disks.Options{}
	if unlockRes.IsEncrypted {
		// then we need to specify that the data mountpoint is expected to be a
		// decrypted device, applies to both ubuntu-data and ubuntu-save
		diskOpts.IsDecryptedDevice = true
	}

	matches, err := disk.MountPointIsFromDisk(boot.InitramfsDataDir, diskOpts)
	if err != nil {
		return err
	}
	if !matches {
		// failed to verify that ubuntu-data mountpoint comes from the same disk
		// as ubuntu-boot
		return fmt.Errorf("cannot validate boot: ubuntu-data mountpoint is expected to be from disk %s but is not", disk.Dev())
	}
	if haveSave {
		diskState.setUnlockStateWithRunKey("ubuntu-save", saveUnlockRes, nil)

		// 4.1a we have ubuntu-save, verify it as well
		matches, err = disk.MountPointIsFromDisk(boot.InitramfsUbuntuSaveDir, diskOpts)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("cannot validate boot: ubuntu-save mountpoint is expected to be from disk %s but is not", disk.Dev())
		}

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

	// All the required disks were unlocked. We now write down
	// their unlock state.
	diskState.serializeTo(boot.UnlockedStateFileName)

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

	typesToMount := typs
	if createSysrootMount() {
		// Create unit for sysroot (mounts either base or rootfs). We
		// restrict this to UC24+ for the moment, until we backport necessary
		// changes to the UC20/22 initramfs. Note that a transient unit is
		// not used as it tries to be restarted after the switch root, and
		// fails.
		typesToMount = []snap.Type{snap.TypeGadget, snap.TypeKernel}
		if isClassic {
			if err := writeSysrootMountUnit(rootfsDir, ""); err != nil {
				return fmt.Errorf("cannot write sysroot.mount (what: %s): %v", rootfsDir, err)
			}
		} else {
			basePlaceInfo := mounts[snap.TypeBase]
			what := filepath.Join(dirs.SnapBlobDirUnder(rootfsDir), basePlaceInfo.Filename())
			if err := writeSysrootMountUnit(what, "squashfs"); err != nil {
				return fmt.Errorf("cannot write sysroot.mount (what: %s): %v", what, err)
			}
		}
	}

	// Create mounts for kernel modules/firmware if we have a drivers tree.
	// InitramfsRunModeSelectSnapsToMount guarantees we do have a kernel in the map.
	kernPlaceInfo := mounts[snap.TypeKernel]
	hasDriversTree, err := createKernelMounts(
		rootfsDir, kernPlaceInfo.SnapName(), kernPlaceInfo.SnapRevision(), isClassic)
	if err != nil {
		return err
	}

	// 4.3 mount the gadget snap and, if there is no drivers tree, the kernel snap
	for _, typ := range typesToMount {
		if typ == snap.TypeKernel && hasDriversTree {
			continue
		}
		sn, ok := mounts[typ]
		if !ok {
			continue
		}
		dir := snapTypeToMountDir[typ]
		snapPath := filepath.Join(dirs.SnapBlobDirUnder(rootfsDir), sn.Filename())
		snapMntPt := filepath.Join(boot.InitramfsRunMntDir, dir)
		if err := doSystemdMount(snapPath, snapMntPt, mountReadOnlyOptions); err != nil {
			return err
		}
		// On 24+ kernels, create /lib/{firmware,modules} mounts if
		// there was no drivers tree. This is a fallback for not really
		// supported but supported cases like having a 24+ kernel with
		// a <24 model. For older initramfs this is done by a
		// generator. Note also that for UC this is done by the
		// extra-paths script, so we need this only for classic.
		if typ == snap.TypeKernel && isClassic && createSysrootMount() {
			logger.Noticef("warning: expected drivers tree not found, mounting /lib/{firmware,modules} directly from kernel snap")
			for _, subDir := range []string{"modules", "firmware"} {
				what := filepath.Join(snapMntPt, subDir)
				where := filepath.Join(dirs.GlobalRootDir, "sysroot", "usr", "lib", subDir)
				if err := writeInitramfsMountUnit(what, where, bindUnit); err != nil {
					return fmt.Errorf("while creating mount for %s in %s: %v",
						what, where, err)
				}
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

		snapdSeed := theSeed.EssentialSnaps()[0]
		if err := setupSeedSnapdSnap(rootfsDir, snapdSeed); err != nil {
			return err
		}
	}

	if createSysrootMount() {
		if err := recalculateRootfsTarget(); err != nil {
			return err
		}
	}

	return nil
}

// setupSeedSnapdSnap makes sure that snapd from the snap is ready to be used
// after switch root when starting from a UC seed.
func setupSeedSnapdSnap(rootfsDir string, snapdSeedSnap *seed.Snap) error {
	// We need to replicate the mount unit that snapd would create, but
	// differently to other mounts we have to do here we do not need to
	// start it from the initramfs. As this is first boot, do it in
	// _writable_defaults to make sure we do not prevent files already
	// there to be copied.
	si := snapdSeedSnap.SideInfo
	// Comes from the seed and it might be unasserted, set revision in that case
	if si.Revision.Unset() {
		si.Revision = snap.R(-1)
	}
	cpi := snap.MinimalSnapContainerPlaceInfo(si.RealName, si.Revision)
	destRoot := sysconfig.WritableDefaultsDir(rootfsDir)
	logger.Debugf("writing %s mount unit to %s", si.RealName, destRoot)
	if err := writeSnapMountUnit(destRoot, snapdSeedSnap.Path, cpi.MountDir(),
		systemd.RegularMountUnit, cpi.MountDescription()); err != nil {
		return fmt.Errorf("while writing %s first boot mount unit: %v", si.RealName, err)
	}

	// We need to initialize /snap/snapd/current symlink so that the
	// dynamic linker
	// /snap/snapd/current/usr/lib/x86_64-linux-gnu/ld-linux-x86-64.so.2 is
	// available to run snapd on first boot.
	mountDir := filepath.Join(rootfsDir, dirs.StripRootDir(dirs.SnapMountDir), si.RealName)
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return err
	}
	return osutil.AtomicSymlink(si.Revision.String(), filepath.Join(mountDir, "current"))
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
