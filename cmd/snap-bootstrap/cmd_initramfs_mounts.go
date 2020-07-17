// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

func init() {
	const (
		short = "Generate initramfs mount tuples"
		long  = "Generate mount tuples for the initramfs until nothing more can be done"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("initramfs-mounts", short, long, &cmdInitramfsMounts{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdInitramfsMounts struct{}

func (c *cmdInitramfsMounts) Execute(args []string) error {
	return generateInitramfsMounts()
}

var (
	osutilIsMounted = osutil.IsMounted

	timeNow = time.Now

	snapTypeToMountDir = map[snap.Type]string{
		snap.TypeBase:   "base",
		snap.TypeKernel: "kernel",
		snap.TypeSnapd:  "snapd",
	}

	secbootMeasureSnapSystemEpochWhenPossible = secboot.MeasureSnapSystemEpochWhenPossible
	secbootMeasureSnapModelWhenPossible       = secboot.MeasureSnapModelWhenPossible
	secbootUnlockVolumeIfEncrypted            = secboot.UnlockVolumeIfEncrypted

	bootFindPartitionUUIDForBootedKernelDisk = boot.FindPartitionUUIDForBootedKernelDisk

	// default 1:30, as that is how long systemd will wait for services by
	// default so seems a sensible default
	defaultMountUnitWaitTimeout = time.Minute + 30*time.Second

	unitFileDependOverride = `[Unit]
Requires=%[1]s
After=%[1]s
`
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
	return ioutil.WriteFile(stampFile, nil, 0644)
}

// SystemdMountOptions reflects the set of options for mounting something using
// systemd-mount(1)
type SystemdMountOptions struct {
	// is tmpfs indicates that "what" should be ignored and a new tmpfs should
	// be mounted at the location
	IsTmpfs bool
	// SurvivesPivotRoot indicates that the mount should persist from the
	// initramfs to after the pivot_root to normal userspace
	// this is done by creating systemd unit .wants symlinks on
	// initrd-device.target in /run
	// TODO: all current instances of doSystemdMount set this to true, should we
	// invert or just drop the option and make it the default behavior ?
	SurvivesPivotRoot bool
	// NeedsFsck indicates that before returning to the caller, an fsck check
	// should be performed on the thing being mounted
	NeedsFsck bool
	// NoWait will not wait until the systemd unit is active and running, which
	// is the default behavior
	NoWait bool
}

var (
	doSystemdMount = doSystemdMountImpl
	counter        = 1
)

// doSystemdMount will mount "what" at "where" using systemd-mount(1) with
// various options
func doSystemdMountImpl(what, where string, opts *SystemdMountOptions) error {
	if opts == nil {
		opts = &SystemdMountOptions{}
	}

	// doesn't make sense to fsck a tmpfs
	if opts.NeedsFsck && opts.IsTmpfs {
		return fmt.Errorf("cannot mount %q at %q: impossible to fsck a tmpfs", what, where)
	}

	whereEscaped := systemd.EscapeUnitNamePath(where)
	unitName := whereEscaped + ".mount"

	args := []string{what, where, "--no-pager", "--no-ask-password"}
	if opts.IsTmpfs {
		args = append(args, "--type=tmpfs")
	}

	if opts.NeedsFsck {
		// note that with the --fsck=yes argument, systemd will block starting
		// the mount unit on a new systemd-fsck@<what> unit that will run the
		// fsck, so we don't need to worry about waiting for that to finish in
		// the case where we are supposed to wait (which is the default for this
		// function)
		args = append(args, "--fsck=yes")
	}

	// Under all circumstances that we use systemd-mount here from
	// snap-bootstrap, it is expected to be okay to block waiting for the unit
	// to be started and become active, because snap-bootstrap is, by design,
	// expected to run as late as possible in the initramfs, and so any
	// dependencies there might be in systemd creating and starting these mount
	// units should already be ready and so we will not block forever. If
	// however there was something going on in systemd at the same time that the
	// mount unit depended on, we could hit a deadlock blocking as systemd will
	// not enqueue this job until it's dependencies are ready, and so if those
	// things depend on this mount unit we are stuck. The solution to this
	// situation is to make snap-bootstrap run as late as possible before
	// mounting things.
	// However, we leave in the option to not block if there is ever a reason
	// we need to do so.
	if opts.NoWait {
		args = append(args, "--no-block")
	}

	// note that we do not currently parse any output from systemd-mount, but if
	// we ever do, take special care surrounding the debug output that systemd
	// outputs with the "debug" kernel command line present (or equivalently the
	// SYSTEMD_LOG_LEVEL=debug env var) which will add lots of additional output
	// to stderr from systemd commands
	out, err := exec.Command("systemd-mount", args...).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(out, err)
	}

	// finally if it should survive pivot_root() then we need to add symlinks
	// to /run/systemd
	if opts.SurvivesPivotRoot {
		// to survive the pivot_root, we need to make the mount units depend on
		// all of the various initrd special targets by adding runtime conf
		// files there
		// note we could do this statically in the initramfs main filesystem
		// layout, but that means that changes to snap-bootstrap would block on
		// waiting for those files to be added before things works here, this is
		// a more flexible strategy that puts snap-bootstrap in control
		for _, initrdUnit := range []string{
			"initrd.target",
			"initrd-fs.target",
			"initrd-switch-root.target",
			"local-fs.target",
		} {
			targetDir := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d")
			err := os.MkdirAll(targetDir, 0755)
			if err != nil {
				return err
			}

			// add an override file for the initrd unit to depend on this mount
			// unit so that when we isolate to the initrd unit, it does not get
			// unmounted
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", whereEscaped)
			content := []byte(fmt.Sprintf(unitFileDependOverride, unitName))
			err = ioutil.WriteFile(filepath.Join(targetDir, fname), content, 0644)
			if err != nil {
				return err
			}
		}
	}

	if !opts.NoWait {
		// TODO: is this necessary, systemd-mount seems to only return when the
		// unit is active and the mount is there, but perhaps we should be a bit
		// paranoid here and wait anyways?
		// see systemd-mount(1)

		// wait for the mount to exist
		start := timeNow()
		var now time.Time
		for now = timeNow(); now.Sub(start) < defaultMountUnitWaitTimeout; now = timeNow() {
			mounted, err := osutilIsMounted(where)
			if mounted {
				break
			}
			if err != nil {
				return err
			}
		}

		if now.Sub(start) > defaultMountUnitWaitTimeout {
			return fmt.Errorf("timed out after 1:30 waiting for mount %s on %s", what, where)
		}
	}

	return nil
}

func generateInitramfsMounts() error {
	// Ensure there is a very early initial measurement
	err := stampedAction("secboot-epoch-measured", func() error {
		return secbootMeasureSnapSystemEpochWhenPossible()
	})
	if err != nil {
		return err
	}

	mode, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}

	mst := newInitramfsMountsState(mode, recoverySystem)

	switch mode {
	case "recover":
		// XXX: don't pass both args
		return generateMountsModeRecover(mst, recoverySystem)
	case "install":
		// XXX: don't pass both args
		return generateMountsModeInstall(mst, recoverySystem)
	case "run":
		return generateMountsModeRun(mst)
	}
	// this should never be reached
	return fmt.Errorf("internal error: mode in generateInitramfsMounts not handled")
}

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(mst *initramfsMountsState, recoverySystem string) error {
	// steps 1 and 2 are shared with recover mode
	err := generateMountsCommonInstallRecover(mst, recoverySystem)
	if err != nil {
		return err
	}

	// 3. final step: write modeenv to tmpfs data dir and disable cloud-init in
	//   install mode
	modeEnv := &boot.Modeenv{
		Mode:           "install",
		RecoverySystem: recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}
	// we need to put the file to disable cloud-init in the
	// _writable_defaults dir for writable-paths(5) to install it properly
	writableDefaultsDir := sysconfig.WritableDefaultsDir(boot.InitramfsWritableDir)
	if err := sysconfig.DisableCloudInit(writableDefaultsDir); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
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
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}
	return nil
}

// copyUbuntuDataAuth copies the authenication files like
//  - extrausers passwd,shadow etc
//  - sshd host configuration
//  - user .ssh dir
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
		// so that the time in recover mode moves forward to what it was in run
		// mode
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

	// ensure the user state is transferred as well
	srcState := filepath.Join(src, "system-data/var/lib/snapd/state.json")
	dstState := filepath.Join(dst, "system-data/var/lib/snapd/state.json")
	err := state.CopyState(srcState, dstState, []string{"auth.users", "auth.macaroon-key", "auth.last-id"})
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("cannot copy user state: %v", err)
	}

	return nil
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

func generateMountsModeRecover(mst *initramfsMountsState, recoverySystem string) error {
	// steps 1 and 2 are shared with install mode
	err := generateMountsCommonInstallRecover(mst, recoverySystem)
	if err != nil {
		return err
	}

	// 3. mount ubuntu-data for recovery
	const lockKeysForLast = true
	device, err := secbootUnlockVolumeIfEncrypted("ubuntu-data", lockKeysForLast)
	if err != nil {
		return err

	}

	opts := &SystemdMountOptions{
		// don't do fsck on the data partition, it could be corrupted

		// always persist /host the mount
		SurvivesPivotRoot: true,
	}

	err = doSystemdMount(device, boot.InitramfsHostUbuntuDataDir, opts)
	if err != nil {
		return err
	}

	// 4. final step: copy the auth data and network config from
	//    the real ubuntu-data dir to the ephemeral ubuntu-data
	//    dir, write the modeenv to the tmpfs data, and disable
	//    cloud-init in recover mode
	if err := copyUbuntuDataAuth(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
		return err
	}
	if err := copyNetworkConfig(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
		return err
	}

	modeEnv := &boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}
	// we need to put the file to disable cloud-init in the
	// _writable_defaults dir for writable-paths(5) to install it properly
	writableDefaultsDir := sysconfig.WritableDefaultsDir(boot.InitramfsWritableDir)
	if err := sysconfig.DisableCloudInit(writableDefaultsDir); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
}

// TODO:UC20: move all of this to a helper in boot?
// selectPartitionToMount will select the partition to mount at dir, preferring
// to use efi variables to determine which partition matches the disk we booted
// the kernel from. If it can't figure out which disk the kernel came from, then
// it will fallback to mounting via the specified label
func selectPartitionMatchingKernelDisk(dir, fallbacklabel string) error {
	// TODO:UC20: should this only run on grade > dangerous? where do we
	//            get the model at this point?
	partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
	// TODO: the by-partuuid is only available on gpt disks, on mbr we need
	//       to use by-uuid or by-id
	partSrc := filepath.Join("/dev/disk/by-partuuid", partuuid)
	if err != nil {
		// no luck, try mounting by label instead
		partSrc = filepath.Join("/dev/disk/by-label", fallbacklabel)
	}

	opts := &SystemdMountOptions{
		// always fsck the partition when we are mounting it, as this is the first
		// partition we will be mounting, we can't know if anything is  corrupted
		// yet
		NeedsFsck: true,
		// always persist the mount
		SurvivesPivotRoot: true,
	}
	return doSystemdMount(partSrc, dir, opts)
}

func generateMountsCommonInstallRecover(mst *initramfsMountsState, recoverySystem string) error {
	// 1. always ensure seed partition is mounted first before the others,
	//      since the seed partition is needed to mount the snap files there
	err := selectPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed")
	if err != nil {
		return err
	}

	// 2.1. measure model
	err = stampedAction(fmt.Sprintf("%s-model-measured", recoverySystem), func() error {
		return secbootMeasureSnapModelWhenPossible(mst.Model)
	})
	if err != nil {
		return err
	}

	// 2.2. (auto) select recovery system and mount seed snaps
	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd}
	essSnaps, err := mst.RecoverySystemEssentialSnaps("", typs)
	if err != nil {
		return fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", typs, err)
	}

	// TODO:UC20: do we need more cross checks here?
	for _, essentialSnap := range essSnaps {
		dir := snapTypeToMountDir[essentialSnap.EssentialType]
		// TODO:UC20: we need to cross-check the kernel path with snapd_recovery_kernel used by grub
		opts := &SystemdMountOptions{
			// always persist the snaps mount
			SurvivesPivotRoot: true,
		}
		err := doSystemdMount(essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, dir), opts)
		if err != nil {
			return err
		}
	}

	// TODO:UC20: after we have the kernel and base snaps mounted, we should do
	//            the bind mounts from the kernel modules on top of the base
	//            mount and delete the corresponding systemd units from the
	//            initramfs layout

	// 2.3. mount "ubuntu-data" on a tmpfs
	opts := &SystemdMountOptions{
		// always persist the tmpfs ubuntu-data mount
		SurvivesPivotRoot: true,
		IsTmpfs:           true,
	}
	return doSystemdMount("tmpfs", boot.InitramfsDataDir, opts)
}

func generateMountsModeRun(mst *initramfsMountsState) error {
	// 1. mount ubuntu-boot
	err := selectPartitionMatchingKernelDisk(boot.InitramfsUbuntuBootDir, "ubuntu-boot")
	if err != nil {
		return err
	}

	// 2. mount ubuntu-seed
	// TODO:UC20: use the ubuntu-boot partition as a reference for what
	//            partition to mount for ubuntu-seed
	opts := &SystemdMountOptions{
		// TODO: do we need to always run fsck on ubuntu-seed in run mode?
		//       would it be safer to not touch ubuntu-seed at all for boot?
		NeedsFsck:         true,
		SurvivesPivotRoot: true,
	}
	err = doSystemdMount("/dev/disk/by-label/ubuntu-seed", boot.InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}

	// 3.1. measure model
	err = stampedAction("run-model-measured", func() error {
		return secbootMeasureSnapModelWhenPossible(mst.UnverifiedBootModel)
	})
	if err != nil {
		return err
	}
	// TODO:UC20: cross check the model we read from ubuntu-boot/model with
	// one recorded in ubuntu-data modeenv during install

	// 3.2. mount Data
	const lockKeysForLast = true
	device, err := secbootUnlockVolumeIfEncrypted("ubuntu-data", lockKeysForLast)
	if err != nil {
		return err
	}

	opts = &SystemdMountOptions{
		// TODO: do we actually need fsck if we are mounting a mapper device?
		// probably not?
		NeedsFsck:         true,
		SurvivesPivotRoot: true,
	}
	err = doSystemdMount(device, boot.InitramfsDataDir, opts)
	if err != nil {
		return err
	}

	// 4.1. read modeenv
	modeEnv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	if err != nil {
		return err
	}

	// 4.2 check if base is mounted
	// 4.3 check if kernel is mounted
	typs := []snap.Type{snap.TypeBase, snap.TypeKernel}

	// 4.4 choose and mount base and kernel snaps (this includes updating modeenv
	//    if needed to try the base snap)
	mounts, err := boot.InitramfsRunModeSelectSnapsToMount(typs, modeEnv)
	if err != nil {
		return err
	}

	// make sure this is a deterministic order
	// TODO:UC20: with grade > dangerous, verify the kernel snap hash against
	//            what we booted using the tpm log, this may need to be passed
	//            to the function above to make decisions there, or perhaps this
	//            code actually belongs in the bootloader implementation itself
	for _, typ := range []snap.Type{snap.TypeBase, snap.TypeKernel} {
		if sn, ok := mounts[typ]; ok {
			dir := snapTypeToMountDir[typ]
			snapPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), sn.Filename())
			opts := &SystemdMountOptions{
				SurvivesPivotRoot: true,
			}
			err := doSystemdMount(snapPath, filepath.Join(boot.InitramfsRunMntDir, dir), opts)
			if err != nil {
				return err
			}
		}
	}

	// 4.5 check if snapd is mounted (only on first-boot will we mount it)
	if modeEnv.RecoverySystem != "" {
		// load the recovery system and generate mount for snapd
		essSnaps, err := mst.RecoverySystemEssentialSnaps(modeEnv.RecoverySystem, []snap.Type{snap.TypeSnapd})
		if err != nil {
			return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
		}

		opts := &SystemdMountOptions{
			SurvivesPivotRoot: true,
		}
		return doSystemdMount(essSnaps[0].Path, filepath.Join(boot.InitramfsRunMntDir, "snapd"), opts)
	}

	return nil
}
