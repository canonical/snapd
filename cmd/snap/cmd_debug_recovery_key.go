// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/secboot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
)

type cmdRecoveryKey struct{}

func init() {
	cmd := addDebugCommand("show-recovery-key",
		"(internal) show the fde recovery key",
		"(internal) show the fde recovery key",
		func() flags.Commander {
			return &cmdRecoveryKey{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdRecoveryKey) Execute(args []string) error {
	if release.OnClassic {
		return errors.New(`command "show-recovery-key" is not available on classic systems`)
	}

	// XXX: should this be defined more centrally? OTOH we have an
	//      integration test that will catch out-of-syncness
	recoveryKeyFile := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	f, err := os.Open(recoveryKeyFile)
	if err != nil {
		return fmt.Errorf("cannot open recovery key: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat %v: %v", recoveryKeyFile, err)
	}
	if st.Size() != int64(len(secboot.RecoveryKey{})) {
		return fmt.Errorf("cannot read recovery key: unexpected size %v for the recovery key file", st.Size())
	}

	var rkey secboot.RecoveryKey
	n, err := f.Read(rkey[:])
	if err != nil {
		return fmt.Errorf("cannot read recovery key: %v", err)
	}
	if n != len(secboot.RecoveryKey{}) {
		return fmt.Errorf("cannot use recovery key: unexpected size %v", n)
	}
	fmt.Fprintf(Stdout, "%s\n", rkey)
	return nil
}
