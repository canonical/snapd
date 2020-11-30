// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/snap"
	"golang.org/x/xerrors"
)

type lk struct {
	rootdir          string
	prepareImageTime bool

	// blDisk is what disk the bootloader informed us to use to look for the
	// bootloader structure partitions
	blDisk disks.Disk

	// role is what bootloader role we are, which also maps to which version of
	// the underlying lkenv struct we use for bootenv
	// * RoleSole == uc16 -> v1
	// * RoleRecovery == uc20 + recovery -> v2 recovery
	// * RoleRunMode == uc20 + run -> v2 run
	role Role
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

// newLk create a new lk bootloader object
func newLk(rootdir string, opts *Options) Bootloader {
	l := &lk{rootdir: rootdir}

	l.processOpts(opts)

	return l
}

func (l *lk) setRootDir(rootdir string) {
	l.rootdir = rootdir
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
		// TODO: this should be adjusted to use the kernel cmdline parameter
		//       for the disk that the bootloader says to find the partition
		//       on, since like the UC20 case, but that involves changing
		//       many more tests, so let's do that in a followup PR
		return filepath.Join(l.rootdir, "/dev/disk/by-partlabel/")
	case RoleRecovery, RoleRunMode:
		// TODO: maybe panic'ing here is a bit harsh...
		panic("internal error: shouldn't be using lk.dir() for uc20 runtime modes!")
	default:
		panic("unexpected bootloader role for lk dir")
	}
}

func (l *lk) InstallBootConfig(gadgetDir string, opts *Options) error {
	// make sure that the opts are put into the object
	l.processOpts(opts)
	gadgetFile := filepath.Join(gadgetDir, l.Name()+".conf")
	systemFile, err := l.envFile()
	if err != nil {
		return err
	}
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (l *lk) Present() (bool, error) {
	partitionLabelOrConfFile := l.partLabelForRoleAndTime()
	// if we are not in runtime mode or in V1, just check the config file
	if l.prepareImageTime || l.role == RoleSole {
		return osutil.FileExists(filepath.Join(l.dir(), partitionLabelOrConfFile)), nil
	}

	// otherwise for V2, non-sole bootloader roles we need to check on the
	// partition name existing, note that envFileForPartName will only return
	// partiallyFound as true if it reasonably concludes that this is a lk
	// device, so in that case forward err, otherwise return err as nil
	_, partiallyFound, err := l.envFileForPartName(partitionLabelOrConfFile)
	if partiallyFound {
		return partiallyFound, err
	}
	return false, nil
}

func (l *lk) partLabelForRoleAndTime() string {
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
	if l.prepareImageTime {
		// conf files at build time have .bin suffix, so technically this now
		// becomes not a partition label but a filename, but meh names are hard
		label += ".bin"
	}
	return label
}

func (l *lk) envFile() (string, error) {
	// as for dir, we have two scenarios, image building and runtime
	partLabel := l.partLabelForRoleAndTime()
	if l.prepareImageTime {
		return filepath.Join(l.dir(), partLabel), nil
	}

	if l.role == RoleSole {
		// see TODO: in l.dir(), this should eventually also be using
		// envFileForPartName() too
		return filepath.Join(l.dir(), partLabel), nil
	}

	// for RoleRun or RoleRecovery, we need to find the partition securely
	envFile, _, err := l.envFileForPartName(partLabel)
	if err != nil {
		return "", err
	}
	return envFile, nil
}

// envFileForPartName returns the environment file in /dev for the partition
// name, which will always be a partition on the disk given by
// the kernel command line parameter "snapd_lk_boot_disk" set by the bootloader.
// It returns a boolean as the second parameter which is primarily used by
// Present() to indicate if the searching process got "far enough" to reasonably
// conclude that the device is using a lk bootloader, but we had errors finding
// it. This feature is mainly for better error reporting in logs.
func (l *lk) envFileForPartName(partName string) (string, bool, error) {
	// lazily initialize l.blDisk if it hasn't yet been initialized
	if l.blDisk == nil {
		// for security, we want to restrict our search for the partition
		// that the binary structure exists on to only the disk that the
		// bootloader tells us to search on - it uses a kernel cmdline
		// parameter "snapd_lk_boot_disk" to indicated which disk we should look
		// for partitions on
		m, err := osutil.KernelCommandLineKeyValues("snapd_lk_boot_disk")
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

	partitionUUID, err := l.blDisk.FindMatchingPartitionUUIDFromPartLabel(partName)
	if err != nil {
		return "", true, err
	}
	return filepath.Join(l.rootdir, "/dev/disk/by-partuuid", partitionUUID), true, nil
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
	f, err := l.envFile()
	if err != nil {
		return nil, err
	}
	return lkenv.NewEnv(f, version), nil
}

func (l *lk) SetBootVars(values map[string]string) error {
	env, err := l.newenv()
	if err != nil {
		return err
	}
	// if we couldn't find the env, that's okay, as this may be the first thing
	// to write boot vars to the env
	if err := env.Load(); err != nil && !xerrors.Is(err, os.ErrNotExist) {
		return err
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

		// TODO:UC20: however this codepath will likely be exercised when we
		//            support creating new recovery systems from runtime
		return fmt.Errorf("internal error: ExtractRecoveryKernelAssets not yet implemented for a runtime lk bootloader")
	}

	env, err := l.newenv()
	if err != nil {
		return err
	}
	// if we couldn't find the env, that's okay, as this may be the first thing
	// to initialize the env when we add the recovery system kernel asset there
	if err := env.Load(); err != nil && !xerrors.Is(err, os.ErrNotExist) {
		return err
	}

	recoverySystem := filepath.Base(recoverySystemDir)

	bootPartition, err := env.FindFreeRecoverySystemBootPartition(recoverySystem)
	if err != nil {
		return err
	}

	// we are preparing a recovery system, just extract boot image to bootloader
	// directory
	logger.Debugf("ExtractRecoveryKernelAssets handling image prepare")
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

	logger.Debugf("ExtractKernelAssets (%s)", blobName)

	env, err := l.newenv()
	if err != nil {
		return err
	}
	// if we couldn't find the env, that's okay, as this may be the first thing
	// to initialize the env when we add the kernel asset there
	if err := env.Load(); err != nil && !xerrors.Is(err, os.ErrNotExist) {
		return err
	}

	bootPartition, err := env.FindFreeKernelBootPartition(blobName)
	if err != nil {
		return err
	}

	if l.prepareImageTime {
		// we are preparing image, just extract boot image to bootloader directory
		logger.Debugf("ExtractKernelAssets handling image prepare")
		if err := snapf.Unpack(env.GetBootImageName(), l.dir()); err != nil {
			return fmt.Errorf("cannot open unpacked %s: %v", env.GetBootImageName(), err)
		}
	} else {
		logger.Debugf("ExtractKernelAssets handling run time usecase")
		// this is live system, extracted bootimg needs to be flashed to
		// free bootimg partition and env has to be updated with
		// new kernel snap to bootimg partition mapping
		tmpdir, err := ioutil.TempDir("", "bootimg")
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
			bpart, _, err = l.envFileForPartName(bootPartition)
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
	logger.Debugf("RemoveKernelAssets (%s)", blobName)
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
