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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

const autoImportsName = "auto-imports.asserts"

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
		mountSrc := l[9]
		// FIXME: premature optimization?
		if !strings.HasPrefix(mountSrc, "/dev/") {
			continue
		}
		if strings.HasPrefix(mountSrc, "/dev/loop") {
			continue
		}
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
			fmt.Fprintf(Stderr, "cannot import %q: %s\n", cand, err)
			continue
		}
		fmt.Fprintf(Stdout, "acked %q\n", cand)
	}

	// FIXME: once we have a way to know if a device is owned
	//        do no longer call this unconditionally
	if added > 0 {
		// FIXME: run `snap create-users --known`
	}

	return nil
}

type cmdAutoImport struct{}

var shortAutoImportHelp = i18n.G("Auto import assertions")

var longAutoImportHelp = i18n.G("This command imports all assertions from block devices that are called 'auto-imports.assertions'")

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
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// SUCKS: racy because the mount is not done when the script is
	//        called
	time.Sleep(1 * time.Second)

	return autoImportFromAllMounts()
}
