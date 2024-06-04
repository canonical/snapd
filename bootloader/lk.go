// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package bootloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap"
)

const (
	backupStorage  = true
	primaryStorage = false
)

type lk struct {
	rootdir          string
	prepareImageTime bool

	// role is what bootloader role we are, which also maps to which version of
	// the underlying lkenv struct we use for bootenv
	// * RoleSole == uc16 -> v1
	// * RoleRecovery == uc20 + recovery -> v2 recovery
	// * RoleRunMode == uc20 + run -> v2 run
	role Role

	// blDisk is what disk the bootloader informed us to use to look for the
	// bootloader structure partitions
	blDisk disks.Disk
}

// newLk create a new lk bootloader object
func newLk(rootdir string, opts *Options) Bootloader {
	l := &lk{rootdir: rootdir}

	l.processOpts(opts)

	return l
}

func (l *lk) processOpts(opts *Options) {
	if opts != nil {
		// XXX: in the long run we want this to go away, we probably add
		//      something like "boot.PrepareImage()" and add an (optional)
		//      method "PrepareImage" to the bootloader interface that is
		//      used to setup a bootloader from prepare-image if things
		//      are very different from runtime vs image-building mode.
		//
		// determine mode we are in, runtime or image build

		l.prepareImageTime = opts.PrepareImageTime

		l.role = opts.Role
	}
}

func (l *lk) Name() string {
	return "lk"
}

func (l *lk) dir() string {
	if l.prepareImageTime {
		// at prepare-image time, then use rootdir and look for /boot/lk/ -
		// this is only used in prepare-image time where the binary files exist
		// extracted from the gadget
		return filepath.Join(l.rootdir, "/boot/lk/")
	}

	// for runtime, we should only be using dir() for V1 and the dir is just
	// the udev by-partlabel directory
	switch l.role {
	case RoleSole:
		// TODO: this should be adjusted to try and use the kernel cmdline
		//       parameter for the disk that the bootloader says to find
		//       the lk partitions on if provided like the UC20 case does, but
		//       that involves changing many more tests, so let's do that in a
		//       followup PR
		return filepath.Join(l.rootdir, "/dev/disk/by-partlabel/")
	case RoleRecovery, RoleRunMode:
		// TODO: maybe panic'ing here is a bit harsh...
		panic("internal error: shouldn't be using lk.dir() for UC20+ runtime modes!")
	default:
		panic("unexpected bootloader role for lk dir")
	}
}

func (l *lk) InstallBootConfig(gadgetDir string, opts *Options) error {
	// make sure that the opts are put into the object
	l.processOpts(opts)
	gadgetFile := filepath.Join(gadgetDir, l.Name()+".conf")
	// since we are just installing static files from the gadget, there is no
	// backup to copy, the backup will be created automatically (if allowed) by
	// lkenv when we go to Save() the environment file.
	systemFile, err := l.envBackstore(primaryStorage)
	if err != nil {
		return err
	}
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (l *lk) Present() (bool, error) {
	// if we are in prepare-image mode or in V1, just check the env file
	if l.prepareImageTime || l.role == RoleSole {
		primary, err := l.envBackstore(primaryStorage)
		if err != nil {
			return false, err
		}

		if osutil.FileExists(primary) {
			return true, nil
		}

		// at prepare-image time, we won't have a backup file from the gadget,
		// so just give up here
		if l.prepareImageTime {
			return false, nil
		}

		// but at runtime we should check the backup in case the primary
		// partition got corrupted
		backup, err := l.envBackstore(backupStorage)
		if err != nil {
			return false, err
		}
		return osutil.FileExists(backup), nil
	}

	// otherwise for V2, non-sole bootloader roles we need to check on the
	// partition name existing, note that devPathForPartName will only return
	// partiallyFound as true if it reasonably concludes that this is a lk
	// device, so in that case forward err, otherwise return err as nil
	partitionLabel := l.partLabelForRole()
	_, partiallyFound, err := l.devPathForPartName(partitionLabel)
	if partiallyFound {
		return true, err
	}
	return false, nil
}

func (l *lk) partLabelForRole() string {
	// TODO: should the partition labels be fetched from gadget.yaml instead? we
	//       have roles that we could use in the gadget.yaml structures to find
	//       them
	label := ""
	switch l.role {
	case RoleSole, RoleRunMode:
		label = "snapbootsel"
	case RoleRecovery:
		label = "snaprecoverysel"
	default:
		panic(fmt.Sprintf("unknown bootloader role for littlekernel: %s", l.role))
	}
	return label
}

// envBackstore returns a filepath for the lkenv bootloader environment file.
// For prepare-image time operations, it will be a normal config file; for
// runtime operations it will be a device file from a udev-created symlink in
// /dev/disk. If backup is true then the filename is suffixed with "bak" or at
// runtime the partition label is suffixed with "bak".
func (l *lk) envBackstore(backup bool) (string, error) {
	partitionLabelOrConfFile := l.partLabelForRole()
	if backup {
		partitionLabelOrConfFile += "bak"
	}
	if l.prepareImageTime {
		// at prepare-image time, we just use the env file, but append .bin
		// since it is a file from the gadget we will evenutally install into
		// a partition when flashing the image
		return filepath.Join(l.dir(), partitionLabelOrConfFile+".bin"), nil
	}

	if l.role == RoleSole {
		// for V1, we just use the partition label directly, dir() here will be
		// the udev by-partlabel symlink dir.
		// see TODO: in l.dir(), this should eventually also be using
		// devPathForPartName() too
		return filepath.Join(l.dir(), partitionLabelOrConfFile), nil
	}

	// for RoleRun or RoleRecovery, we need to find the partition securely
	partitionFile, _, err := l.devPathForPartName(partitionLabelOrConfFile)
	if err != nil {
		return "", err
	}
	return partitionFile, nil
}

// devPathForPartName returns the environment file in /dev for the partition
// name, which will always be a partition on the disk given by
// the kernel command line parameter "snapd_lk_boot_disk" set by the bootloader.
// It returns a boolean as the second parameter which is primarily used by
// Present() to indicate if the searching process got "far enough" to reasonably
// conclude that the device is using a lk bootloader, but we had errors finding
// it. This feature is mainly for better error reporting in logs.
func (l *lk) devPathForPartName(partName string) (string, bool, error) {
	// lazily initialize l.blDisk if it hasn't yet been initialized
	if l.blDisk == nil {
		// For security, we want to restrict our search for the partition
		// that the binary structure exists on to only the disk that the
		// bootloader tells us to search on - it uses a kernel cmdline
		// parameter "snapd_lk_boot_disk" to indicated which disk we should look
		// for partitions on. In typical boot scenario this will be something like
		// "snapd_lk_boot_disk=mmcblk0".
		m, err := kcmdline.KeyValues("snapd_lk_boot_disk")
		if err != nil {
			// return false, since we don't have enough info to conclude there
			// is likely a lk bootloader here or not
			return "", false, err
		}
		blDiskName, ok := m["snapd_lk_boot_disk"]
		if blDiskName == "" {
			// we switch on ok here, since if "snapd_lk_boot_disk" was found at
			// all on the kernel command line, we can reasonably assume that
			// only the lk bootloader would have put it there, but maybe
			// it is buggy and put an empty value there.
			if ok {
				return "", true, fmt.Errorf("kernel command line parameter \"snapd_lk_boot_disk\" is empty")
			}
			// if we didn't find the kernel command line parameter at all, then
			// we want to return false because we don't have enough info
			return "", false, fmt.Errorf("kernel command line parameter \"snapd_lk_boot_disk\" is missing")
		}

		disk, err := disks.DiskFromDeviceName(blDiskName)
		if err != nil {
			return "", true, fmt.Errorf("cannot find disk from bootloader supplied disk name %q: %v", blDiskName, err)
		}

		l.blDisk = disk
	}

	partitionUUID, err := l.blDisk.FindMatchingPartitionUUIDWithPartLabel(partName)
	if err != nil {
		return "", true, err
	}

	// for the runtime lk bootloader we should never prefix the path with the
	// bootloader rootdir and instead always use dirs.GlobalRootDir, since the
	// file we are providing is at an absolute location for all bootloaders,
	// regardless of role, in /dev, so using dirs.GlobalRootDir ensures that we
	// are still able to mock things in test functions, but that we never end up
	// trying to use a path like /run/mnt/ubuntu-boot/dev/disk/by-partuuid/...
	// for example
	return filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partuuid", partitionUUID), true, nil
}

func (l *lk) newenv() (*lkenv.Env, error) {
	// check which role we are, it affects which struct is used for the env
	var version lkenv.Version
	switch l.role {
	case RoleSole:
		version = lkenv.V1
	case RoleRecovery:
		version = lkenv.V2Recovery
	case RoleRunMode:
		version = lkenv.V2Run
	}
	f, err := l.envBackstore(primaryStorage)
	if err != nil {
		return nil, err
	}

	backup, err := l.envBackstore(backupStorage)
	if err != nil {
		return nil, err
	}

	return lkenv.NewEnv(f, backup, version), nil
}

func (l *lk) GetBootVars(names ...string) (map[string]string, error) {
	out := make(map[string]string)

	env, err := l.newenv()
	if err != nil {
		return nil, err
	}
	if err := env.Load(); err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (l *lk) SetBootVars(values map[string]string) error {
	env, err := l.newenv()
	if err != nil {
		return err
	}
	// if we couldn't find the env, that's okay, as this may be the first thing
	// to write boot vars to the env
	if err := env.Load(); err != nil {
		// if the error was something other than file not found, it is fatal
		if !xerrors.Is(err, os.ErrNotExist) {
			return err
		}
		// otherwise at prepare-image time it is okay to not have the file
		// existing, but we should always have it at runtime as it is a
		// partition, so it is highly unexpected for it to be missing and we
		// cannot proceed
		// also note that env.Load() will automatically try the backup, so if
		// Load() failed to get the backup at runtime there's nothing left to
		// try here
		if !l.prepareImageTime {
			return err
		}
	}

	// update environment only if something changes
	dirty := false
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirty = true
	}

	if dirty {
		return env.Save()
	}

	return nil
}

func (l *lk) ExtractRecoveryKernelAssets(recoverySystemDir string, sn snap.PlaceInfo, snapf snap.Container) error {
	if !l.prepareImageTime {
		// error case, we cannot be extracting a recovery kernel and also be
		// called with !opts.PrepareImageTime (yet)

		// TODO:UC20: this codepath is exercised when creating new
		// recovery systems from runtime
		return fmt.Errorf("internal error: extracting recovery kernel assets is not supported for a runtime lk bootloader")
	}

	env, err := l.newenv()
	if err != nil {
		return err
	}
	if err := env.Load(); err != nil {
		// don't handle os.ErrNotExist specially here, it doesn't really make
		// sense to extract kernel assets if we can't load the existing env,
		// since then the caller would just see an error about not being able
		// to find the kernel blob name (as they will all be empty in the env),
		// when in reality the reason one can't find an available boot image
		// partition is because we couldn't read the env file and so returning
		// that error is better
		return err
	}

	recoverySystem := filepath.Base(recoverySystemDir)

	bootPartition, err := env.FindFreeRecoverySystemBootPartition(recoverySystem)
	if err != nil {
		return err
	}

	// we are preparing a recovery system, just extract boot image to bootloader
	// directory
	logger.Debugf("extracting recovery kernel %s to %s with lk bootloader", sn.SnapName(), recoverySystem)
	if err := snapf.Unpack(env.GetBootImageName(), l.dir()); err != nil {
		return fmt.Errorf("cannot open unpacked %s: %v", env.GetBootImageName(), err)
	}

	if err := env.SetBootPartitionRecoverySystem(bootPartition, recoverySystem); err != nil {
		return err
	}

	return env.Save()
}

// ExtractKernelAssets extract kernel assets per bootloader specifics
// lk bootloader requires boot partition to hold valid boot image
// there are two boot partition available, one holding current bootimage
// kernel assets are extracted to other (free) boot partition
// in case this function is called as part of image creation,
// boot image is extracted to the file
func (l *lk) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	blobName := s.Filename()

	logger.Debugf("extracting kernel assets for %s with lk bootloader", s.SnapName())

	env, err := l.newenv()
	if err != nil {
		return err
	}
	if err := env.Load(); err != nil {
		// don't handle os.ErrNotExist specially here, it doesn't really make
		// sense to extract kernel assets if we can't load the existing env,
		// since then the caller would just see an error about not being able
		// to find the kernel blob name (as they will all be empty in the env),
		// when in reality the reason one can't find an available boot image
		// partition is because we couldn't read the env file and so returning
		// that error is better
		return err
	}

	bootPartition, err := env.FindFreeKernelBootPartition(blobName)
	if err != nil {
		return err
	}

	if l.prepareImageTime {
		// we are preparing image, just extract boot image to bootloader directory
		if err := snapf.Unpack(env.GetBootImageName(), l.dir()); err != nil {
			return fmt.Errorf("cannot open unpacked %s: %v", env.GetBootImageName(), err)
		}
	} else {
		// this is live system, extracted bootimg needs to be flashed to
		// free bootimg partition and env has to be updated with
		// new kernel snap to bootimg partition mapping
		tmpdir, err := os.MkdirTemp("", "bootimg")
		if err != nil {
			return fmt.Errorf("cannot create temp directory: %v", err)
		}
		defer os.RemoveAll(tmpdir)

		bootImg := env.GetBootImageName()
		if err := snapf.Unpack(bootImg, tmpdir); err != nil {
			return fmt.Errorf("cannot unpack %s: %v", bootImg, err)
		}
		// write boot.img to free boot partition
		bootimgName := filepath.Join(tmpdir, bootImg)
		bif, err := os.Open(bootimgName)
		if err != nil {
			return fmt.Errorf("cannot open unpacked %s: %v", bootImg, err)
		}
		defer bif.Close()
		var bpart string
		// TODO: for RoleSole bootloaders this will eventually be the same
		// codepath as for non-RoleSole bootloader
		if l.role == RoleSole {
			bpart = filepath.Join(l.dir(), bootPartition)
		} else {
			bpart, _, err = l.devPathForPartName(bootPartition)
			if err != nil {
				return err
			}
		}

		bpf, err := os.OpenFile(bpart, os.O_WRONLY, 0660)
		if err != nil {
			return fmt.Errorf("cannot open boot partition [%s]: %v", bpart, err)
		}
		defer bpf.Close()

		if _, err := io.Copy(bpf, bif); err != nil {
			return err
		}
	}

	if err := env.SetBootPartitionKernel(bootPartition, blobName); err != nil {
		return err
	}

	return env.Save()
}

func (l *lk) RemoveKernelAssets(s snap.PlaceInfo) error {
	blobName := s.Filename()
	logger.Debugf("removing kernel assets for %s with lk bootloader", s.SnapName())

	env, err := l.newenv()
	if err != nil {
		return err
	}
	if err := env.Load(); err != nil {
		// don't handle os.ErrNotExist specially here, it doesn't really make
		// sense to delete kernel assets if we can't load the existing env,
		// since then the caller would just see an error about not being able
		// to find the kernel blob name, when in reality the reason one can't
		// find that kernel blob name is because we couldn't read the env file
		return err
	}
	err = env.RemoveKernelFromBootPartition(blobName)
	if err == nil {
		// found and removed the revision from the bootimg matrix, need to
		// update the env to persist the change
		return env.Save()
	}
	return nil
}
