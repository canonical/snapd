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

package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/mkfs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

var (
	mkfsImpl                      = mkfs.Make
	kernelEnsureKernelDriversTree = kernel.EnsureKernelDriversTree
)

type mkfsParams struct {
	Type       string
	Device     string
	Label      string
	Size       quantity.Size
	SectorSize quantity.Size
}

// makeFilesystem creates a filesystem on the on-disk structure, according
// to the filesystem type defined in the gadget. If sectorSize is specified,
// that sector size is used when creating the filesystem, otherwise if it is
// zero, automatic values are used instead.
func makeFilesystem(params mkfsParams) error {
	logger.Debugf("create %s filesystem on %s with label %q", params.Type, params.Device, params.Label)
	if err := mkfsImpl(params.Type, params.Device, params.Label, params.Size, params.SectorSize); err != nil {
		return err
	}
	return udevTrigger(params.Device)
}

type mntfsParams struct {
	NoExec bool
	NoDev  bool
	NoSuid bool
}

func (p *mntfsParams) flags() uintptr {
	var flags uintptr
	if p.NoDev {
		flags |= syscall.MS_NODEV
	}
	if p.NoExec {
		flags |= syscall.MS_NOEXEC
	}
	if p.NoSuid {
		flags |= syscall.MS_NOSUID
	}
	return flags
}

// mountFilesystem mounts the filesystem on a given device with
// filesystem type fs under the provided mount point directory.
func mountFilesystem(fsDevice, fs, mountpoint string, params mntfsParams) error {
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return fmt.Errorf("cannot create mountpoint: %v", err)
	}
	if err := sysMount(fsDevice, mountpoint, fs, params.flags(), ""); err != nil {
		return fmt.Errorf("cannot mount filesystem %q at %q: %v", fsDevice, mountpoint, err)
	}
	return nil
}

func unmountWithFallbackToLazy(mntPt, operationMsg string) error {
	if err := sysUnmount(mntPt, 0); err != nil {
		logger.Noticef("cannot unmount %s after %s: %v (trying lazy unmount next)", mntPt, operationMsg, err)
		// lazy umount on error, see LP:2025402
		if err = sysUnmount(mntPt, syscall.MNT_DETACH); err != nil {
			logger.Noticef("cannot lazy unmount %q: %v", mntPt, err)
			return err
		}
	}
	return nil
}

// writeContent populates the given on-disk filesystem structure with a
// corresponding filesystem device, according to the contents defined in the
// gadget.
func writeFilesystemContent(laidOut *gadget.LaidOutStructure, kSnapInfo *KernelSnapInfo, fsDevice string, observer gadget.ContentObserver) (err error) {
	mountpoint := filepath.Join(dirs.SnapRunDir, "gadget-install", strings.ReplaceAll(strings.Trim(fsDevice, "/"), "/", "-"))
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	// temporarily mount the filesystem
	logger.Debugf("mounting %q in %q (fs type %q)", fsDevice, mountpoint, laidOut.Filesystem())
	if err := sysMount(fsDevice, mountpoint, laidOut.Filesystem(), 0, ""); err != nil {
		return fmt.Errorf("cannot mount %q at %q: %v", fsDevice, mountpoint, err)
	}
	defer func() {
		errUnmount := unmountWithFallbackToLazy(mountpoint, "writing filesystem content")
		if err == nil && errUnmount != nil {
			err = fmt.Errorf("cannot unmount %v after writing filesystem content: %v", fsDevice, errUnmount)
		}
	}()
	fs, err := gadget.NewMountedFilesystemWriter(nil, laidOut, observer)
	if err != nil {
		return fmt.Errorf("cannot create filesystem image writer: %v", err)
	}

	var noFilesToPreserve []string
	if err := fs.Write(mountpoint, noFilesToPreserve); err != nil {
		return fmt.Errorf("cannot create filesystem image: %v", err)
	}

	// For data partition, build drivers tree and kernel snap mount units if
	// required, so kernel drivers are available on first boot of the installed
	// system. In case we have a preseeding tarball files with the same content
	// will be in there. handle-writable-paths will then overwite them, but will
	// not have any effect as are expected to be equal.
	// TODO detect if we have a preseeding tarball to avoid the extra unnecessary
	// work.
	if laidOut.Role() == gadget.SystemData && kSnapInfo != nil && kSnapInfo.NeedsDriversTree {
		destRoot := mountpoint
		if kSnapInfo.IsCore {
			// For core we write the changes in _writable_defaults. The
			// files are then copied from the initramfs by
			// populate-writable.service on first boot as the directories
			// they are in are marked as "transitional" in the
			// writable-paths file. We cannot copy directly to
			// "system-data" as that would prevent files already in
			// _writable_defaults in the directories of interest to not
			// be copied (because the files are not copied if the
			// directory exists already).
			destRoot = sysconfig.WritableDefaultsDir(filepath.Join(mountpoint, "system-data"))
		}
		destDir := kernel.DriversTreeDir(destRoot, kSnapInfo.Name, kSnapInfo.Revision)
		logger.Noticef("building drivers tree in %s", destDir)

		// kernel-modules components that are needed to build the drivers tree
		compsMntPts := make([]kernel.ModulesCompMountPoints, 0, len(kSnapInfo.ModulesComps))
		for _, c := range kSnapInfo.ModulesComps {
			cpi := snap.MinimalComponentContainerPlaceInfo(c.Name,
				c.Revision, kSnapInfo.Name)
			compsMntPts = append(compsMntPts, kernel.ModulesCompMountPoints{
				LinkName: c.Name,
				MountPoints: kernel.MountPoints{
					Current: c.MountPoint,
					Target:  cpi.MountDir(),
				}})
			// Create mount unit to make the component content
			// available from the drivers tree.
			if err := writeContainerMountUnit(destRoot, cpi); err != nil {
				return err
			}
		}

		cpi := snap.MinimalSnapContainerPlaceInfo(kSnapInfo.Name, kSnapInfo.Revision)
		// Create mount unit to make the kernel snap content available from
		// the drivers tree.
		if err := writeContainerMountUnit(destRoot, cpi); err != nil {
			return err
		}

		if err := kernelEnsureKernelDriversTree(
			kernel.MountPoints{
				Current: kSnapInfo.MountPoint,
				Target:  cpi.MountDir(),
			},
			compsMntPts,
			destDir,
			&kernel.KernelDriversTreeOptions{KernelInstall: true}); err != nil {
			return err
		}
	}

	return nil
}

func writeContainerMountUnit(destRoot string, cpi snap.ContainerPlaceInfo) error {
	// Create mount unit to make the kernel snap content available from
	// the drivers tree.
	squashfsPath := dirs.StripRootDir(cpi.MountFile())
	whereDir := dirs.StripRootDir(cpi.MountDir())

	hostFsType, options := systemd.HostFsTypeAndMountOptions("squashfs")
	mountOptions := &systemd.MountUnitOptions{
		Lifetime:                 systemd.Persistent,
		Description:              cpi.MountDescription(),
		What:                     squashfsPath,
		Where:                    whereDir,
		Fstype:                   hostFsType,
		Options:                  options,
		MountUnitType:            systemd.BeforeDriversLoadMountUnit,
		RootDir:                  destRoot,
		PreventRestartIfModified: true,
	}
	unitFileName, _, err := systemd.EnsureMountUnitFileContent(mountOptions)
	if err != nil {
		return err
	}
	// Make sure the unit is activated
	unitFilePath := filepath.Join(dirs.SnapServicesDir, unitFileName)
	for _, target := range []string{"multi-user.target.wants", "snapd.mounts.target.wants"} {
		linkDir := filepath.Join(dirs.SnapServicesDirUnder(destRoot), target)
		if err := os.MkdirAll(linkDir, 0755); err != nil {
			return err
		}
		linkPath := filepath.Join(linkDir, unitFileName)
		if err := os.Symlink(unitFilePath, linkPath); err != nil {
			if !os.IsExist(err) {
				return err
			}

			// if we already have a file at linkPath, make sure that it is a
			// symlink that points to unitFilePath
			if err := checkLinkPointsTo(linkPath, unitFilePath); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkLinkPointsTo(linkPath string, expectedTarget string) error {
	info, err := os.Lstat(linkPath)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("existing path at %q is not a symlink", linkPath)
	}

	target, err := os.Readlink(linkPath)
	if err != nil {
		return err
	}

	if target != expectedTarget {
		return fmt.Errorf("existing symlink at %q points to %q, expected %q", linkPath, target, expectedTarget)
	}
	return nil
}
