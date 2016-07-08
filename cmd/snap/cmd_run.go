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
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
)

var (
	syscallExec = syscall.Exec
	userCurrent = user.Current
)

type cmdRun struct {
	Positional struct {
		SnapApp string `positional-arg-name:"<app name>" description:"the snap (e.g. hello-world) or application to run (e.g. hello-world.env)"`
	} `positional-args:"yes" required:"yes"`

	Command  string `long:"command" description:"alternative command to run" hidden:"yes"`
	Hook     string `long:"hook" description:"hook to run" hidden:"yes"`
	Revision string `short:"r" description:"use a specific snap revision when running hook" default:"unset" hidden:"yes"`
	Shell    bool   `long:"shell" description:"run a shell instead of the command (useful for debugging)"`
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
		return fmt.Errorf("cannot use --hook and --command together")
	}
	if x.Revision != "" && x.Hook == "" {
		return fmt.Errorf("-r can only be used with --hook")
	}
	if x.Hook != "" && len(args) > 0 {
		return fmt.Errorf("too many arguments for hook %q: %s", x.Hook, strings.Join(args, " "))
	}

	// Now actually handle the dispatching
	if x.Hook != "" {
		return snapRunHook(x.Positional.SnapApp, x.Revision, x.Hook)
	}

	// pass shell as a special command to snap-exec
	if x.Shell {
		x.Command = "shell"
	}

	return snapRunApp(x.Positional.SnapApp, x.Command, args)
}

func getSnapInfo(snapName string, revision snap.Revision) (*snap.Info, error) {
	if revision.Unset() {
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

func createUserDataDirs(info *snap.Info) error {
	usr, err := userCurrent()
	if err != nil {
		return fmt.Errorf("cannot get the current user: %s", err)
	}

	// see snapenv.User
	userData := filepath.Join(usr.HomeDir, info.MountDir())
	commonUserData := filepath.Join(userData, "..", "common")
	for _, d := range []string{userData, commonUserData} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("cannot create %q: %s", d, err)
		}
	}
	return nil
}

func snapRunApp(snapApp, command string, args []string) error {
	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := getSnapInfo(snapName, snap.R(0))
	if err != nil {
		return err
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf("cannot find app %q in %q", appName, snapName)
	}

	return runSnapConfine(info, app.SecurityTag(), snapApp, command, args)
}

func snapRunHook(snapName, snapRevision, hookName string) error {
	revision, err := snap.ParseRevision(snapRevision)
	if err != nil {
		return err
	}

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

func runSnapConfine(info *snap.Info, securityTag, binary, command string, args []string) error {
	if err := createUserDataDirs(info); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

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

	return syscallExec(cmd[0], cmd, env)
}
