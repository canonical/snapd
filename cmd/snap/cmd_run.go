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
	"path/filepath"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
)

type cmdRun struct {
	Positional struct {
		SnapApp string `positional-arg-name:"<app name>" description:"the snap (e.g. hello-world) or application to run (e.g. hello-world.env)"`
	} `positional-args:"yes" required:"yes"`

	Command  string `long:"command" description:"alternative command to run" hidden:"yes"`
	Hook     string `long:"hook" description:"hook to run" hidden:"yes"`
	Revision string `long:"revision" description:"use a specific snap revision instead of the active one (this only applies when using --hook)" hidden:"yes"`
}

func init() {
	addCommand("run",
		i18n.G("Run the given snap command"),
		i18n.G("Run the given snap command with the right confinement and environment"),
		func() flags.Commander {
			return &cmdRun{}
		})
}

func (x *cmdRun) Execute(args []string) error {
	// Catch some invalid parameter combinations, provide helpful errors
	if x.Hook != "" && x.Command != "" {
		return fmt.Errorf("invalid parameters: --hook cannot be used with --command")
	}
	if x.Revision != "" && x.Hook == "" {
		return fmt.Errorf("invalid parameters: --revision can only be used with --hook")
	}
	if x.Hook != "" && len(args) > 0 {
		return fmt.Errorf("invalid parameters: extra arguments cannot be used when using --hook")
	}

	// Now actually handle the dispatching
	if x.Hook != "" {
		return snapRunHook(x.Positional.SnapApp, x.Hook, x.Revision)
	}

	return snapRunApp(x.Positional.SnapApp, x.Command, args)
}

func getSnapInfo(snapName string, snapRevision string) (*snap.Info, error) {
	var revision snap.Revision
	if snapRevision != "" {
		// User supplied a revision.
		var err error
		revision, err = snap.ParseRevision(snapRevision)
		if err != nil {
			return nil, fmt.Errorf("invalid revision: %q", snapRevision)
		}
	} else {
		// User didn't supply a revision, so we need to get it via the snapd API
		// here because once we're inside the confinement it may be unavailable.
		snaps, err := Client().List([]string{snapName})
		if err != nil {
			return nil, err
		}
		if len(snaps) == 0 {
			return nil, fmt.Errorf("cannot find snap %q", snapName)
		}
		if len(snaps) > 1 {
			return nil, fmt.Errorf("multiple snaps for %q: %d", snapName, len(snaps))
		}
		revision = snaps[0].Revision
	}

	info, err := snap.ReadInfo(snapName, &snap.SideInfo{
		Revision: revision,
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}

// returns the environment that is important for the later stages of execution
// (like SNAP_REVISION that snap-exec requires to work)
func snapExecEnv(info *snap.Info) []string {
	env := snapenv.Basic(info)
	env = append(env, snapenv.User(info, os.Getenv("HOME"))...)
	return env
}

func snapRunApp(snapApp, command string, args []string) error {
	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := getSnapInfo(snapName, "")
	if err != nil {
		return err
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf("cannot find app %q in %q", appName, snapName)
	}

	return runSnapConfine(info, app.SecurityTag(), snapApp, command, args)
}

func snapRunHook(snapName, hookName, revision string) error {
	info, err := getSnapInfo(snapName, revision)
	if err != nil {
		return err
	}

	hook := info.Hooks[hookName]
	if hook == nil {
		return fmt.Errorf("cannot find hook %q in %q", hookName, snapName)
	}

	hookBinary := filepath.Join(info.HooksDir(), hook.Name)

	return runSnapConfine(info, hook.SecurityTag(), hookBinary, "", nil)
}

var SyscallExec = syscall.Exec

func runSnapConfine(info *snap.Info, securityTag, binary, command string, args []string) error {
	cmd := []string{
		"/usr/bin/ubuntu-core-launcher",
		securityTag,
		securityTag,
		"/usr/lib/snapd/snap-exec",
		binary,
	}

	if command != "" {
		cmd = append(cmd, "--command="+command)
	}

	cmd = append(cmd, args...)

	env := append(os.Environ(), snapExecEnv(info)...)

	return SyscallExec(cmd[0], cmd, env)
}
