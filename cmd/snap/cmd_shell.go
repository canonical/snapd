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
	"fmt"
	"os"
	"syscall"

	//"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

type cmdShell struct {
	Positional struct {
		ShellType string
	} `positional-args:"yes"`
}

// FIXME: reenable for GA
/*
func init() {
	addCommand("shell",
		i18n.G("Run snappy shell interface"),
		i18n.G("Run snappy shell interface"),
		func() flags.Commander {
			return &cmdShell{}
		}, nil, []argDesc{{
			name: i18n.G("<shell-type>"),
			desc: i18n.G("The type of shell you want"),
		}})
}
*/

// reexec will reexec itself with sudo
func reexecWithSudo() error {
	args := []string{"/usr/bin/sudo"}
	args = append(args, os.Args...)
	env := os.Environ()
	if err := syscall.Exec(args[0], args, env); err != nil {
		return fmt.Errorf("failed to exec classic shell: %s", err)
	}
	panic("this should never be reached")
}

func (x *cmdShell) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	shellType := x.Positional.ShellType

	// FIXME: make this generic so that all snaps can provide a
	//        shell
	if shellType == "classic" {
		if !osutil.FileExists("/snap/classic/current") {
			return fmt.Errorf(i18n.G(`Classic dimension disabled on this system.
Use "sudo snap install --devmode classic && sudo classic.create" to enable it.`))
		}

		// we need to re-exec if we do not run as root
		if os.Getuid() != 0 {
			if err := reexecWithSudo(); err != nil {
				return err
			}
		}

		fmt.Fprintln(Stdout, i18n.G(`Entering classic dimension`))
		fmt.Fprintln(Stdout, i18n.G(`

The home directory is shared between snappy and the classic dimension.
Run "exit" to leave the classic shell.
`))
		args := []string{"/snap/bin/classic.shell"}
		return syscall.Exec(args[0], args, os.Environ())
	}

	return fmt.Errorf(i18n.G("unsupported shell %v"), shellType)
}
