// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"crypto"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snapdenv"
)

const autoImportsName = "auto-import.assert"

func autoImportCandidates() ([]string, error) {
	var cands []string

	isTesting := snapdenv.Testing()

	mnts, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, fmt.Errorf("couldn't parse mountinfo: %v", err)
	}
	for _, mnt := range mnts {
		// skip everything that is not a device (cgroups, debugfs etc)
		if !strings.HasPrefix(mnt.MountSource, "/dev/") {
			continue
		}
		// skip all loop devices (snaps)
		if strings.HasPrefix(mnt.MountSource, "/dev/loop") {
			continue
		}
		// skip all ram disks (unless in tests)
		if !isTesting && strings.HasPrefix(mnt.MountSource, "/dev/ram") {
			continue
		}

		// TODO: should the following 2 checks try to be more smart like
		//       `snap-bootstrap initramfs-mounts` and try to find the boot disk
		//       and determine what partitions to skip using the disks package?

		// skip all initramfs mounted disks on uc20
		mountPoint := mnt.MountDir
		if strings.HasPrefix(mountPoint, boot.InitramfsRunMntDir) {
			continue
		}

		// skip all seed dir mount points too, as these are bind mounts to the
		// initramfs dirs on uc20, this can show up as
		// /writable/system-data/var/lib/snapd/seed as well as
		// /var/lib/snapd/seed
		if strings.HasPrefix(mountPoint, dirs.SnapSeedDir) {
			continue
		}

		// TODO: we should probably make this a formal dir in dirs.go, but it is
		// not directly used since we just use SnapSeedDir instead
		writableSystemDataDir := filepath.Join(dirs.GlobalRootDir, "writable", "system-data")
		if strings.HasPrefix(mountPoint, dirs.SnapSeedDirUnder(writableSystemDataDir)) {
			continue
		}

		cand := filepath.Join(mountPoint, autoImportsName)
		if osutil.FileExists(cand) {
			cands = append(cands, cand)
		}
	}

	return cands, nil
}

func queueFile(src string) error {
	// refuse huge files, this is for assertions
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	// 640kb ought be to enough for anyone
	if fi.Size() > 640*1024 {
		msg := fmt.Errorf("cannot queue %s, file size too big: %v", src, fi.Size())
		logger.Noticef("error: %v", msg)
		return msg
	}

	// ensure name is predictable, weak hash is ok
	hash, _, err := osutil.FileDigest(src, crypto.SHA3_384)
	if err != nil {
		return err
	}

	dst := filepath.Join(dirs.SnapAssertsSpoolDir, fmt.Sprintf("%s.assert", base64.URLEncoding.EncodeToString(hash)))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	return osutil.CopyFile(src, dst, osutil.CopyFlagOverwrite)
}

func autoImportFromSpool(cli *client.Client) (added int, err error) {
	files, err := os.ReadDir(dirs.SnapAssertsSpoolDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	for _, fi := range files {
		cand := filepath.Join(dirs.SnapAssertsSpoolDir, fi.Name())
		if err := ackFile(cli, cand); err != nil {
			logger.Noticef("error: cannot import %s: %s", cand, err)
			continue
		} else {
			logger.Noticef("imported %s", cand)
			added++
		}
		// FIXME: only remove stuff older than N days?
		if err := os.Remove(cand); err != nil {
			return 0, err
		}
	}

	return added, nil
}

func autoImportFromAllMounts(cli *client.Client) (int, error) {
	cands, err := autoImportCandidates()
	if err != nil {
		return 0, err
	}

	added := 0
	for _, cand := range cands {
		err := ackFile(cli, cand)
		// the server is not ready yet
		if _, ok := err.(client.ConnectionError); ok {
			logger.Noticef("queuing for later %s", cand)
			if err := queueFile(cand); err != nil {
				return 0, err
			}
			continue
		}
		if err != nil {
			logger.Noticef("error: cannot import %s: %s", cand, err)
			continue
		} else {
			logger.Noticef("imported %s", cand)
		}
		added++
	}

	return added, nil
}

var osMkdirTemp = os.MkdirTemp

func tryMount(deviceName string) (string, error) {
	tmpMountTarget, err := osMkdirTemp("", "snapd-auto-import-mount-")
	if err != nil {
		err = fmt.Errorf("cannot create temporary mount point: %v", err)
		logger.Noticef("error: %v", err)
		return "", err
	}
	// udev does not provide much environment ;)
	if os.Getenv("PATH") == "" {
		os.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	}
	// not using syscall.Mount() because we don't know the fs type in advance
	cmd := exec.Command("mount", "-t", "ext4,vfat", "-o", "ro", "--make-private", deviceName, tmpMountTarget)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpMountTarget)
		err = fmt.Errorf("cannot mount %s: %s", deviceName, osutil.OutputErr(output, err))
		logger.Noticef("error: %v", err)
		return "", err
	}

	return tmpMountTarget, nil
}

var syscallUnmount = syscall.Unmount

func doUmount(mp string) error {
	if err := syscallUnmount(mp, 0); err != nil {
		return err
	}
	return os.Remove(mp)
}

type cmdAutoImport struct {
	clientMixin
	Mount []string `long:"mount" arg-name:"<device path>"`

	ForceClassic bool `long:"force-classic"`
}

var shortAutoImportHelp = i18n.G("Inspect devices for actionable information")

var longAutoImportHelp = i18n.G(`
The auto-import command searches available mounted devices looking for
assertions that are signed by trusted authorities, and potentially
performs system changes based on them.

If one or more device paths are provided via --mount, these are temporarily
mounted to be inspected as well. Even in that case the command will still
consider all available mounted devices for inspection.

Assertions to be imported must be made available in the auto-import.assert file
in the root of the filesystem.
`)

func init() {
	cmd := addCommand("auto-import",
		shortAutoImportHelp,
		longAutoImportHelp,
		func() flags.Commander {
			return &cmdAutoImport{}
		}, map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"mount": i18n.G("Temporarily mount device before inspecting"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"force-classic": i18n.G("Force import on classic systems"),
		}, nil)
	cmd.hidden = true
}

func (x *cmdAutoImport) autoAddUsers() error {
	options := client.CreateUserOptions{
		Automatic: true,
	}
	results, err := x.client.CreateUsers([]*client.CreateUserOptions{&options})
	for _, result := range results {
		fmt.Fprintf(Stdout, i18n.G("created user %q\n"), result.Username)
	}

	return err
}

func removableBlockDevices() (removableDevices []string) {
	// eg. /sys/block/sda/removable
	removable, err := filepath.Glob(filepath.Join(dirs.GlobalRootDir, "/sys/block/*/removable"))
	if err != nil {
		return nil
	}
	for _, removableAttr := range removable {
		val, err := os.ReadFile(removableAttr)
		if err != nil || string(val) != "1\n" {
			// non removable
			continue
		}
		// let's see if it has partitions
		dev := filepath.Base(filepath.Dir(removableAttr))

		pattern := fmt.Sprintf(filepath.Join(dirs.GlobalRootDir, "/sys/block/%s/%s*/partition"), dev, dev)
		// eg. /sys/block/sda/sda1/partition
		partitionAttrs, _ := filepath.Glob(pattern)

		if len(partitionAttrs) == 0 {
			// not partitioned? try to use the main device
			removableDevices = append(removableDevices, fmt.Sprintf("/dev/%s", dev))
			continue
		}

		for _, partAttr := range partitionAttrs {
			val, err := os.ReadFile(partAttr)
			if err != nil || string(val) != "1\n" {
				// non partition?
				continue
			}
			pdev := filepath.Base(filepath.Dir(partAttr))
			removableDevices = append(removableDevices, fmt.Sprintf("/dev/%s", pdev))
			// hasPartitions = true
		}
	}
	sort.Strings(removableDevices)
	return removableDevices
}

// inInstallmode returns true if it's UC20 system in install/factory-reset modes
func inInstallMode() bool {
	modeenv, err := boot.ReadModeenv(dirs.GlobalRootDir)
	if err != nil {
		return false
	}
	return modeenv.Mode == "install" || modeenv.Mode == "factory-reset"
}

func (x *cmdAutoImport) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if release.OnClassic && !x.ForceClassic {
		fmt.Fprintf(Stderr, "auto-import is disabled on classic\n")
		return nil
	}
	// TODO:UC20: workaround for LP: #1860231
	if inInstallMode() {
		fmt.Fprintf(Stderr, "auto-import is disabled in install modes\n")
		return nil
	}

	devices := x.Mount
	if len(devices) == 0 {
		// coldplug scenario, try all removable devices
		devices = removableBlockDevices()
	}

	for _, path := range devices {
		// udev adds new /dev/loopX devices on the fly when a
		// loop mount happens and there is no loop device left.
		//
		// We need to ignore these events because otherwise both
		// our mount and the "mount -o loop" fight over the same
		// device and we get nasty errors
		if strings.HasPrefix(path, "/dev/loop") {
			continue
		}

		mp, err := tryMount(path)
		if err != nil {
			continue // Error was reported. Continue looking.
		}
		defer doUmount(mp)
	}

	added1, err := autoImportFromSpool(x.client)
	if err != nil {
		return err
	}

	added2, err := autoImportFromAllMounts(x.client)
	if err != nil {
		return err
	}

	if added1+added2 > 0 {
		return x.autoAddUsers()
	}

	return nil
}
