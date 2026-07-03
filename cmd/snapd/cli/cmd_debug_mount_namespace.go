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
The debug mount-namespace command displays information about the mount namespace
of a given snap when invoked with no flags. Use --shell to start a shell or run
a command inside the mount namespace. Use --discard to discard the mount
namespace of a snap if one exists.

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

func snapMountNamespacePath(snapName string) (string, error) {
	mntPath := filepath.Join(dirs.SnapRunNsDir, snapName+".mnt")
	if _, err := os.Stat(mntPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("cannot stat mount namespace file: %w", err)
	}
	return mntPath, nil
}

func (x *cmdDebugMountNamespace) Execute(args []string) error {
	if x.Shell && x.Discard {
		return fmt.Errorf("--shell and --discard cannot be used together")
	}

	if x.Discard {
		return x.discard(x.Positional.Snap)
	}

	if x.Shell {
		return x.shell(x.Positional.Snap, args)
	}

	mntPath, err := snapMountNamespacePath(x.Positional.Snap)
	if err != nil {
		return err
	}
	if mntPath != "" {
		fmt.Fprintf(Stdout, "mount namespace of snap %q bound to %s\n",
			x.Positional.Snap, mntPath)
	} else {
		fmt.Fprintf(Stdout, "no mount namespace of snap %q found\n",
			x.Positional.Snap)
	}
	return nil
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
	mntPath, err := snapMountNamespacePath(snapName)
	if err != nil {
		return err
	}
	if mntPath == "" {
		return fmt.Errorf("cannot enter mount namespace of snap %q: "+
			"the mount namespace is not bound to a file (%s does not exist)",
			snapName, filepath.Join(dirs.SnapRunNsDir, snapName+".mnt"))
	}

	nsenterPath, err := exec.LookPath("nsenter")
	if err != nil {
		return fmt.Errorf("cannot find nsenter: %v", err)
	}

	argv := []string{"nsenter", "-m" + mntPath}
	if len(args) > 0 {
		argv = append(argv, args...)
	} else {
		argv = append(argv, "/bin/bash")
	}

	env := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	return syscallExec(nsenterPath, argv, env)
}
