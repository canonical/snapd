// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snapdtool"
)

var (
	osGetuid = os.Getuid
)

type cmdDebugMountNamespace struct{}

func init() {
	cmd := addDebugCommand("mount-namespace",
		"Debugging of snap mount namespaces",
		"Commands to aid debugging of snap mount namespaces.",
		func() flags.Commander { return &cmdDebugMountNamespace{} },
		nil,
		nil,
	)
	cmd.extra = func(c *flags.Command) {
		c.AddCommand("discard", "Discard a snap mount namespace",
			"Discard the mount namespace of the given snap by invoking snap-discard-ns.",
			&cmdDebugMountNsDiscard{})
		c.AddCommand("shell", "Start a shell or run a command in a snap mount namespace",
			`Run a command inside the mount namespace of the given snap using nsenter.
If no command is specified, an interactive shell (/bin/bash) is started.`,
			&cmdDebugMountNsShell{})
	}
}

func (x *cmdDebugMountNamespace) Execute(args []string) error {
	return flag.ErrHelp
}

type cmdDebugMountNsDiscard struct {
	Positionals struct {
		SnapName string `required:"yes" positional-arg-name:"<snap-name>"`
	} `positional-args:"true"`
}

func (x *cmdDebugMountNsDiscard) Execute(args []string) error {
	if osGetuid() != 0 {
		return fmt.Errorf("this command requires root privileges")
	}

	toolPath, err := snapdtool.InternalToolPath("snap-discard-ns")
	if err != nil {
		return fmt.Errorf("cannot find snap-discard-ns: %v", err)
	}

	cmd := exec.Command(toolPath, x.Positionals.SnapName)
	cmd.Stdout = Stdout
	cmd.Stderr = Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot discard mount namespace of snap %q: %v",
			x.Positionals.SnapName, err)
	}
	return nil
}

type cmdDebugMountNsShell struct {
	Positionals struct {
		SnapName string `required:"yes" positional-arg-name:"<snap-name>"`
	} `positional-args:"true"`
}

func (x *cmdDebugMountNsShell) Execute(args []string) error {
	if osGetuid() != 0 {
		return fmt.Errorf("this command requires root privileges")
	}

	mntPath := filepath.Join(dirs.SnapRunNsDir, x.Positionals.SnapName+".mnt")
	if _, err := os.Stat(mntPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cannot enter mount namespace of snap %q: "+
				"the mount namespace is not bound to a file (%s does not exist)",
				x.Positionals.SnapName, mntPath)
		}
		return fmt.Errorf("cannot stat mount namespace file: %w", err)
	}

	nsenterPath, err := exec.LookPath("nsenter")
	if err != nil {
		return fmt.Errorf("cannot find nsenter: %v", err)
	}

	argv := []string{"nsenter", "-m" + mntPath}
	if len(args) > 0 {
		argv = append(argv, args...)
	} else {
		// We know that /bin/bash is in the core* snaps. If a given uses
		// bare, then the user can always invoke '/bin/busybox sh'.
		argv = append(argv, "/bin/bash")
	}

	// Override the environment and set a safe PATH.
	env := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	return syscallExec(nsenterPath, argv, env)
}
