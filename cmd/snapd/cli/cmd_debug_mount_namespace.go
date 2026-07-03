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

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snapdtool"
)

var shortDebugMountNamespaceHelp = i18n.G("Debug and inspect snap mount namespaces")

var longDebugMountNamespaceHelp = i18n.G(`
Run a command or start a shell inside the mount namespace of the given snap.

When used with --discard, discard the mount namespace of a snap if one exists.

The command may require root privileges.
`)

type cmdDebugMountNamespace struct {
	Shell      bool `long:"shell"`
	Discard    bool `long:"discard"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>" required:"yes"`
	} `positional-args:"yes"`
}

func init() {
	addDebugCommand("mount-namespace",
		shortDebugMountNamespaceHelp,
		longDebugMountNamespaceHelp,
		func() flags.Commander { return &cmdDebugMountNamespace{} },
		map[string]string{
			"shell":   i18n.G("Open a shell or execute a command within the mount namespace"),
			"discard": i18n.G("Discard the mount namespace"),
		},
		[]argDesc{
			{"<snap>", "Snap name"},
		},
	)
}

func (x *cmdDebugMountNamespace) Execute(args []string) error {
	if x.Shell && x.Discard {
		return fmt.Errorf("--shell and --discard cannot be used together")
	}

	if x.Discard {
		return x.discard(x.Positional.Snap)
	}

	// --shell is default
	return x.shell(x.Positional.Snap, args)
}

func (x *cmdDebugMountNamespace) discard(snapName string) error {
	toolPath, err := snapdtool.InternalToolPath("snap-discard-ns")
	if err != nil {
		return fmt.Errorf("cannot find snap-discard-ns: %v", err)
	}

	cmd := exec.Command(toolPath, snapName)
	cmd.Stdout = Stdout
	cmd.Stderr = Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot discard mount namespace of snap %q: %w",
			snapName, err)
	}
	return nil
}

func (x *cmdDebugMountNamespace) shell(snapName string, args []string) error {
	mntPath := filepath.Join(dirs.SnapRunNsDir, snapName+".mnt")
	if _, err := os.Stat(mntPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cannot enter mount namespace of snap %q: "+
				"the mount namespace is not bound to a file (%s does not exist)",
				snapName, mntPath)
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
