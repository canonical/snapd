// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/systemd"
)

var (
	timeNow = time.Now

	// default 1:30, as that is how long systemd will wait for services by
	// default so seems a sensible default.
	defaultMountUnitWaitTimeout = time.Minute + 30*time.Second

	unitFileDependOverride = `[Unit]
Wants=%[1]s
`

	doSystemdMount = doSystemdMountImpl
)

// forbiddenChars is a list of characters that are not allowed in any mount paths used in systemd-mount.
const forbiddenChars = `\,:" `

type fsOpts interface {
	AppendOptions([]string) ([]string, error)
}

// overlayFsOptions groups the options to systemd-mount related to overlayfs.
type overlayFsOptions struct {
	// Directories to be used as lower layers of an overlay mount.
	// It does not need to be on a writable filesystem.
	LowerDirs []string
	// Optional. A directory to be used as the upper layer of an overlay mount.
	// This is normally on a writable filesystem.
	UpperDir string
	// Optional. A directory to be used as the workdir of an overlay mount.
	// This needs to be an empty directory on the same filesystem as upperdir.
	WorkDir string
}

// validate is used to perform consistency checks on the options related to overlayfs mounts.
func (o *overlayFsOptions) validate() error {
	if len(o.LowerDirs) <= 0 {
		return errors.New("missing arguments for overlayfs mount: at least one lowerdir is required")
	}

	if len(o.UpperDir) > 0 && len(o.WorkDir) <= 0 {
		return errors.New("an upperdir for an overlayfs mount was specified but workdir is missing")
	}

	if len(o.WorkDir) > 0 && len(o.UpperDir) <= 0 {
		return errors.New("a workdir for an overlayfs mount was specified but upperdir is missing")
	}

	if strings.ContainsAny(o.UpperDir, forbiddenChars) {
		return fmt.Errorf("upperdir overlayfs mount option contains forbidden characters. %q contains one of %q", o.UpperDir, forbiddenChars)
	}

	if strings.ContainsAny(o.WorkDir, forbiddenChars) {
		return fmt.Errorf("workdir overlayfs mount option contains forbidden characters. %q contains one of %q", o.WorkDir, forbiddenChars)
	}

	for _, d := range o.LowerDirs {
		if strings.ContainsAny(d, forbiddenChars) {
			return fmt.Errorf("lowerdir overlayfs mount option contains forbidden characters. %q contains one of %q", d, forbiddenChars)
		}
	}

	return nil
}

// AppendOptions constructs the overlayfs related arguments to systemd-mount after validation.
func (o *overlayFsOptions) AppendOptions(options []string) ([]string, error) {
	err := o.validate()
	if err != nil {
		return nil, err
	}

	// This is used for splitting multiple lowerdirs as done in
	// https://elixir.bootlin.com/linux/v6.10.9/C/ident/ovl_parse_param_split_lowerdirs.
	lowerDirs := strings.Join(o.LowerDirs, ":")

	// options = append(options, fmt.Sprintf("lowerdir=%s", lowerDirs.String()))
	options = append(options, fmt.Sprintf("lowerdir=%s", lowerDirs))
	options = append(options, fmt.Sprintf("upperdir=%s", o.UpperDir))
	options = append(options, fmt.Sprintf("workdir=%s", o.WorkDir))

	return options, nil
}

// dmVerityOptions groups the options to systemd-mount related to dm-verity.
type dmVerityOptions struct {
	// dm-verity hash device
	HashDevice string
	// dm-verity root hash
	RootHash string
	// dm-verity hash offset. Need to be specified if only verity data are
	// appended to the snap. Defaults to 0 in mount command.
	HashOffset uint64
}

// validate is used to perform consistency checks on the options related to dm-verity mounts.
func (o *dmVerityOptions) validate() error {
	if o.HashDevice != "" && o.RootHash == "" {
		return errors.New("mount with dm-verity was requested but a root hash was not specified")
	}
	if o.RootHash != "" && o.HashDevice == "" {
		return errors.New("mount with dm-verity was requested but a hash device was not specified")
	}

	if strings.ContainsAny(o.HashDevice, forbiddenChars) {
		return fmt.Errorf("dm-verity hash device path contains forbidden characters. %q contains one of %q", o.HashDevice, forbiddenChars)
	}

	if o.HashOffset != 0 && (o.HashDevice == "" || o.RootHash == "") {
		return errors.New("mount with dm-verity was requested but a hash device and root hash were not specified")
	}

	return nil
}

// AppendOptions constructs the dm-verity related arguments to systemd-mount after validation.
func (o *dmVerityOptions) AppendOptions(options []string) ([]string, error) {
	err := o.validate()
	if err != nil {
		return nil, err
	}

	if o.HashDevice != "" && o.RootHash != "" {
		options = append(options, fmt.Sprintf("verity.roothash=%s", o.RootHash))
		options = append(options, fmt.Sprintf("verity.hashdevice=%s", o.HashDevice))

		if o.HashOffset != 0 {
			options = append(options, fmt.Sprintf("verity.hashoffset=%d", o.HashOffset))
		}
	}

	return options, nil
}

// systemdMountOptions reflects the set of options for mounting something using
// systemd-mount(1)
type systemdMountOptions struct {
	// Tmpfs indicates that "what" should be ignored and a new tmpfs should be
	// mounted at the location.
	Tmpfs bool
	// Ephemeral indicates that the mount should not persist from the initramfs
	// to after the pivot_root to normal userspace. The default value, false,
	// means that the mount will persist across the transition, this is done by
	// creating systemd unit overrides for various initrd targets in /run that
	// systemd understands when it isolates to the initrd-cleanup.target when
	// the pivot_root is performed.
	Ephemeral bool
	// NeedsFsck indicates that before returning to the caller, an fsck check
	// should be performed on the thing being mounted.
	NeedsFsck bool
	// NoWait will not wait until the systemd unit is active and running, which
	// is the default behavior.
	NoWait bool
	// NoSuid indicates that the partition should be mounted with nosuid set on
	// it to prevent suid execution.
	NoSuid bool
	// NoDev indicates to not interpret character or block special devices on
	// the file system.
	NoDev bool
	// NoExec indicates to not allow direct execution of any binaries on the
	// mounted file system.
	NoExec bool
	// Bind indicates a bind mount.
	Bind bool
	// Read-only mount
	ReadOnly bool
	// Private mount
	Private bool
	// Umount the mountpoint
	Umount bool
	// FsOpts groups additional options for the mount such as overlayfs or
	// dm-verity related options.
	FsOpts fsOpts
}

// doSystemdMount will mount "what" at "where" using systemd-mount(1) with
// various options. Note that in some error cases, the mount unit may have
// already been created and it will not be deleted here, if that is the case
// callers should check manually if the unit needs to be removed on error
// conditions.
func doSystemdMountImpl(what, where string, opts *systemdMountOptions) error {
	if opts == nil {
		opts = &systemdMountOptions{}
	}

	// doesn't make sense to fsck a tmpfs
	if opts.NeedsFsck && opts.Tmpfs {
		return fmt.Errorf("cannot mount %q at %q: impossible to fsck a tmpfs", what, where)
	}

	whereEscaped := systemd.EscapeUnitNamePath(where)
	unitName := whereEscaped + ".mount"

	if opts.Tmpfs && what == "" {
		what = "tmpfs"
	}

	args := []string{what, where, "--no-pager", "--no-ask-password"}

	if opts.Umount {
		args = []string{where, "--umount", "--no-pager", "--no-ask-password"}
	}

	if opts.Tmpfs {
		args = append(args, "--type=tmpfs")
	}

	if opts.NeedsFsck {
		// note that with the --fsck=yes argument, systemd will block starting
		// the mount unit on a new systemd-fsck@<what> unit that will run the
		// fsck, so we don't need to worry about waiting for that to finish in
		// the case where we are supposed to wait (which is the default for this
		// function).
		args = append(args, "--fsck=yes")
	} else {
		// the default is to use fsck=yes, so if it doesn't need fsck we need to
		// explicitly turn it off.
		args = append(args, "--fsck=no")
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

	var options []string
	if opts.NoDev {
		options = append(options, "nodev")
	}
	if opts.NoSuid {
		options = append(options, "nosuid")
	}
	if opts.NoExec {
		options = append(options, "noexec")
	}
	if opts.Bind {
		options = append(options, "bind")
	}
	if opts.ReadOnly {
		options = append(options, "ro")
	}
	if opts.Private {
		options = append(options, "private")
	}

	if opts.FsOpts != nil {
		switch o := opts.FsOpts.(type) {
		case *overlayFsOptions, *dmVerityOptions:
			if _, ok := o.(*overlayFsOptions); ok {
				args = append(args, "--type=overlay")
			}

			var err error
			options, err = o.AppendOptions(options)
			if err != nil {
				return fmt.Errorf("cannot mount %q at %q: %w", what, where, err)
			}
		default:
			return fmt.Errorf("cannot mount %q at %q: invalid options", what, where)
		}
	}

	if len(options) > 0 {
		args = append(args, "--options="+strings.Join(options, ","))
	}

	// if it should survive pivot_root() then we need to add overrides for this
	// unit to /run/systemd units
	if !opts.Ephemeral {
		// to survive the pivot_root, mounts need to be "wanted" by
		// initrd-switch-root.target directly or indirectly. The
		// proper target to place them in is initrd-fs.target
		// note we could do this statically in the initramfs main filesystem
		// layout, but that means that changes to snap-bootstrap would block on
		// waiting for those files to be added before things works here, this is
		// a more flexible strategy that puts snap-bootstrap in control.
		overrideContent := []byte(fmt.Sprintf(unitFileDependOverride, unitName))
		for _, initrdUnit := range []string{"initrd-fs.target", "local-fs.target"} {
			targetDir := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d")
			err := os.MkdirAll(targetDir, 0755)
			if err != nil {
				return err
			}

			// add an override file for the initrd unit to depend on this mount
			// unit so that when we isolate to the initrd unit, it does not get
			// unmounted
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", whereEscaped)
			err = os.WriteFile(filepath.Join(targetDir, fname), overrideContent, 0644)
			if err != nil {
				return err
			}
		}
		// local-fs.target is already automatically a dependency.
		args = append(args, "--property=Before=initrd-fs.target")
	}

	stdout, stderr, err := osutil.RunSplitOutput("systemd-mount", args...)
	if err != nil {
		return osutil.OutputErrCombine(stdout, stderr, err)
	}

	if !opts.NoWait {
		// TODO: is this necessary, systemd-mount seems to only return when the
		// unit is active and the mount is there, but perhaps we should be a bit
		// paranoid here and wait anyways?
		// see systemd-mount(1)

		// wait for the mount to exist.
		start := timeNow()
		var now time.Time
		for now = timeNow(); now.Sub(start) < defaultMountUnitWaitTimeout; now = timeNow() {
			mounted, err := osutilIsMounted(where)
			if mounted == !opts.Umount {
				break
			}
			if err != nil {
				return err
			}
		}

		if now.Sub(start) > defaultMountUnitWaitTimeout {
			return fmt.Errorf("timed out after %s waiting for mount %s on %s", defaultMountUnitWaitTimeout, what, where)
		}
	}

	return nil
}

const driversUnit = `[Unit]
Description=Mount of kernel drivers tree
DefaultDependencies=no
After=initrd-parse-etc.service
Before=initrd-fs.target
Before=umount.target
Conflicts=umount.target

[Mount]
What=%[1]s
Where=%[2]s
Options=bind,shared
`

const containerUnit = `[Unit]
Description=Mount for kernel snap
DefaultDependencies=no
After=initrd-parse-etc.service
Before=initrd-fs.target
Before=umount.target
Conflicts=umount.target

[Mount]
What=%[1]s
Where=%[2]s
Type=%[3]s
Options=%[4]s
`

type unitType string

const (
	bindUnit     unitType = "bind"
	squashfsUnit unitType = "squashfs"
)

func writeInitramfsMountUnit(what, where string, utype unitType) error {
	what = dirs.StripRootDir(what)
	where = dirs.StripRootDir(where)
	unitDir := dirs.SnapRuntimeServicesDirUnder(dirs.GlobalRootDir)
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return err
	}
	var unit string
	switch utype {
	case bindUnit:
		unit = fmt.Sprintf(driversUnit, what, where)
	case squashfsUnit:
		hostFsType, options := systemd.HostFsTypeAndMountOptions("squashfs")
		unit = fmt.Sprintf(containerUnit, what, where, hostFsType, strings.Join(options, ","))
	default:
		return fmt.Errorf("internal error, unknown unit type %s", utype)
	}
	unitFileName := systemd.EscapeUnitNamePath(where) + ".mount"
	unitPath := filepath.Join(unitDir, unitFileName)
	// This is in /run, no need for atomic writes
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return err
	}

	// Pull the unit from initrd-fs.target
	wantsDir := filepath.Join(unitDir, "initrd-fs.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}
	linkPath := filepath.Join(wantsDir, unitFileName)
	return os.Symlink(filepath.Join("..", unitFileName), linkPath)
}

func assembleSysrootMountUnitContent(what, mntType string) string {
	var typ string
	if mntType == "" {
		typ = "Type=none\nOptions=bind"
	} else {
		typ = fmt.Sprintf("Type=%s", mntType)
	}
	content := fmt.Sprintf(`[Unit]
DefaultDependencies=no
Before=initrd-root-fs.target
After=snap-initramfs-mounts.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=%[1]s
Where=/sysroot
%[2]s
`, what, typ)
	return content
}

func writeSysrootMountUnit(what, mntType string) error {
	// Writing the unit in this folder overrides
	// /lib/systemd/system/sysroot-writable.mount - we will remove
	// this unit eventually from the initramfs.
	unitDir := dirs.SnapRuntimeServicesDirUnder(dirs.GlobalRootDir)
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return err
	}
	unitFileName := "sysroot.mount"
	unitPath := filepath.Join(unitDir, unitFileName)
	what = dirs.StripRootDir(what)
	unitContent := assembleSysrootMountUnitContent(what, mntType)
	// This is in the initramfs, no need for atomic writes
	if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
		return err
	}

	// Pull the unit from initrd-root-fs.target
	wantsDir := filepath.Join(unitDir, "initrd-root-fs.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}
	linkPath := filepath.Join(wantsDir, unitFileName)
	if err := os.Symlink(filepath.Join("..", unitFileName), linkPath); err != nil &&
		!os.IsExist(err) {
		return err
	}
	return nil
}

// writeSnapMountUnit writes a mount unit for a snap but does not activate it.
// It uses destRoot as rootfs under which to store the unit.
func writeSnapMountUnit(destRoot, what, where string, unitType systemd.MountUnitType, description string) error {
	hostFsType, options := systemd.HostFsTypeAndMountOptions("squashfs")
	mountOptions := &systemd.MountUnitOptions{
		Lifetime:                 systemd.Persistent,
		Description:              description,
		What:                     dirs.StripRootDir(what),
		Where:                    dirs.StripRootDir(where),
		Fstype:                   hostFsType,
		Options:                  options,
		MountUnitType:            unitType,
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
		if err := osutil.AtomicSymlink(unitFilePath, linkPath); err != nil {
			return err
		}
	}

	return nil
}
