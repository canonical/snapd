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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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
		if len(l) == 0 {
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

func autoImportFromAllMounts() error {
	cands, err := autoImportCandidates()
	if err != nil {
		return err
	}

	added := 0
	for _, cand := range cands {
		if err := ackFile(cand); err != nil {
			logger.Noticef("error: cannot import %s: %s", cand, err)
		} else {
			logger.Noticef("imported %s", cand)
		}
	}

	if added > 0 {
		cli := Client()
		isManaged, err := cli.IsManaged()
		if err != nil {
			logger.Noticef("error calling IsManaged: %v", err)
		} else if !isManaged {
			options := client.CreateUserOptions{
				Sudoer: true,
				Known:  true,
			}

			_, err = cli.CreateUsers([]*client.CreateUserOptions{&options})
		}
	}

	return err
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
	cmd := exec.Command("mount", "-o", "ro", "--make-private", deviceName, tmpMountTarget)
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
}

var shortAutoImportHelp = i18n.G("Inspects devices for actionable information")

var longAutoImportHelp = i18n.G(`
The auto-import command searches available mounted devices looking for
assertions that are signed by trusted authorities, and potentially
performs system changes based on them.

If one or more device paths are provided via --mount, these are temporariy
mounted to be inspected as well. Even in that case the command will still
consider all available mounted devices for inspection.

Imported assertions must be made available in the auto-import.assert file
in the root of the filesystem.
`)

func init() {
	cmd := addCommand("auto-import",
		shortAutoImportHelp,
		longAutoImportHelp,
		func() flags.Commander {
			return &cmdAutoImport{}
		}, map[string]string{
			"mount": i18n.G("Temporarily mount device before inspecting"),
		}, nil)
	cmd.hidden = true
}

func (x *cmdAutoImport) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	for _, path := range x.Mount {
		mp, err := tryMount(path)
		if err != nil {
			continue // Error was reported. Continue looking.
		}
		defer doUmount(mp)
	}

	return autoImportFromAllMounts()
}
