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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

const autoImportsName = "auto-imports.assert"

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
			logger.Noticef("cannot import %q: %s\n", cand, err)
			continue
		}
		logger.Noticef("acked %q\n", cand)

	}

	// FIXME: once we have a way to know if a device is owned
	//        do no longer call this unconditionally
	if added > 0 {
		// FIXME: run `snap create-users --known`
	}

	return nil
}

func tryMount(deviceName string) (string, error) {
	tmpMountTarget, err := ioutil.TempDir("", "snapd-auto-import-mount-")
	if err != nil {
		msg := "cannot create tmp mount point"
		logger.Noticef(msg)
		return "", fmt.Errorf(msg)
	}
	// udev does not provide much environment ;)
	if os.Getenv("PATH") == "" {
		os.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	}
	// not using syscall.Mount() because we don't know the fs type in
	// advance
	cmd := exec.Command("mount", "-o", "ro", "--make-private", deviceName, tmpMountTarget)
	if output, err := cmd.CombinedOutput(); err != nil {
		msg := fmt.Sprintf("cannot mount %q: %s", deviceName, osutil.OutputErr(output, err))
		logger.Panicf(msg)
		return "", fmt.Errorf(msg)
	}

	return tmpMountTarget, nil
}

func doUmount(mp string) error {
	if err := syscall.Unmount(mp, 0); err != nil {
		return err
	}
	return os.Remove(mp)
}

type cmdAutoImport struct{}

var shortAutoImportHelp = i18n.G("Imports assertions from mounted devices")

var longAutoImportHelp = i18n.G("The auto-import command imports assertions found in the auto-import.assert file in mounted devices.")

func init() {
	cmd := addCommand("auto-import",
		shortAutoImportHelp,
		longAutoImportHelp,
		func() flags.Commander {
			return &cmdAutoImport{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdAutoImport) Execute(args []string) error {
	if len(args) > 1 {
		return ErrExtraArgs
	}
	if len(args) > 0 {
		mp, err := tryMount(args[0])
		if err != nil {
			return err
		}
		defer doUmount(mp)
	}

	return autoImportFromAllMounts()
}
