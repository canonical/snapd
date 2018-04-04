// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"bufio"
	"crypto"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

const autoImportsName = "auto-import.assert"

var mountInfoPath = "/proc/self/mountinfo"

func autoImportCandidates() ([]string, error) {
	var cands []string

	// see https://www.kernel.org/doc/Documentation/filesystems/proc.txt,
	// sec. 3.5
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		l := strings.Fields(scanner.Text())

		// Per proc.txt:3.5, /proc/<pid>/mountinfo looks like
		//
		//  36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
		//  (1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
		//
		// and (7) has zero or more elements, find the "-" separator.
		i := 6
		for i < len(l) && l[i] != "-" {
			i++
		}
		if i+2 >= len(l) {
			continue
		}

		mountSrc := l[i+2]

		// skip everything that is not a device (cgroups, debugfs etc)
		if !strings.HasPrefix(mountSrc, "/dev/") {
			continue
		}
		// skip all loop devices (snaps)
		if strings.HasPrefix(mountSrc, "/dev/loop") {
			continue
		}
		// skip all ram disks (unless in tests)
		if !osutil.GetenvBool("SNAPPY_TESTING") && strings.HasPrefix(mountSrc, "/dev/ram") {
			continue
		}

		mountPoint := l[4]
		cand := filepath.Join(mountPoint, autoImportsName)
		if osutil.FileExists(cand) {
			cands = append(cands, cand)
		}
	}

	return cands, scanner.Err()

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

func autoImportFromSpool() (added int, err error) {
	files, err := ioutil.ReadDir(dirs.SnapAssertsSpoolDir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	for _, fi := range files {
		cand := filepath.Join(dirs.SnapAssertsSpoolDir, fi.Name())
		if err := ackFile(cand); err != nil {
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

func autoImportFromAllMounts() (int, error) {
	cands, err := autoImportCandidates()
	if err != nil {
		return 0, err
	}

	added := 0
	for _, cand := range cands {
		err := ackFile(cand)
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

func tryMount(deviceName string) (string, error) {
	tmpMountTarget, err := ioutil.TempDir("", "snapd-auto-import-mount-")
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

func doUmount(mp string) error {
	if err := syscall.Unmount(mp, 0); err != nil {
		return err
	}
	return os.Remove(mp)
}

type cmdAutoImport struct {
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
			"mount":         i18n.G("Temporarily mount device before inspecting"),
			"force-classic": i18n.G("Force import on classic systems"),
		}, nil)
	cmd.hidden = true
}

func autoAddUsers() error {
	cmd := cmdCreateUser{
		Known: true, Sudoer: true,
	}
	return cmd.Execute(nil)
}

func (x *cmdAutoImport) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if release.OnClassic && !x.ForceClassic {
		fmt.Fprintf(Stderr, "auto-import is disabled on classic\n")
		return nil
	}

	for _, path := range x.Mount {
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

	added1, err := autoImportFromSpool()
	if err != nil {
		return err
	}

	added2, err := autoImportFromAllMounts()
	if err != nil {
		return err
	}

	if added1+added2 > 0 {
		return autoAddUsers()
	}

	return nil
}
