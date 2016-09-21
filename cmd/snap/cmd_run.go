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
	Command  string `long:"command" hidden:"yes"`
	Hook     string `long:"hook" hidden:"yes"`
	Revision string `short:"r" default:"unset" hidden:"yes"`
	Shell    bool   `long:"shell" `
}

func init() {
	addCommand("run",
		i18n.G("Run the given snap command"),
		i18n.G("Run the given snap command with the right confinement and environment"),
		func() flags.Commander {
			return &cmdRun{}
		}, map[string]string{
			"command": i18n.G("Alternative command to run"),
			"hook":    i18n.G("Hook to run"),
			"r":       i18n.G("Use a specific snap revision when running hook"),
			"shell":   i18n.G("Run a shell instead of the command (useful for debugging)"),
		}, nil)
}

func (x *cmdRun) Execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(i18n.G("need the application to run as argument"))
	}
	snapApp := args[0]
	args = args[1:]

	// Catch some invalid parameter combinations, provide helpful errors
	if x.Hook != "" && x.Command != "" {
		return fmt.Errorf(i18n.G("cannot use --hook and --command together"))
	}
	if x.Revision != "unset" && x.Revision != "" && x.Hook == "" {
		return fmt.Errorf(i18n.G("-r can only be used with --hook"))
	}
	if x.Hook != "" && len(args) > 0 {
		// TRANSLATORS: %q is the hook name; %s a space-separated list of extra arguments
		return fmt.Errorf(i18n.G("too many arguments for hook %q: %s"), x.Hook, strings.Join(args, " "))
	}

	// Now actually handle the dispatching
	if x.Hook != "" {
		return snapRunHook(snapApp, x.Revision, x.Hook)
	}

	// pass shell as a special command to snap-exec
	if x.Shell {
		x.Command = "shell"
	}

	return snapRunApp(snapApp, x.Command, args)
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
			return nil, fmt.Errorf(i18n.G("multiple snaps for %q: %d"), snapName, len(snaps))
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

func createUserDataDirs(info *snap.Info) error {
	usr, err := userCurrent()
	if err != nil {
		return fmt.Errorf(i18n.G("cannot get the current user: %v"), err)
	}

	// see snapenv.User
	userData := info.UserDataDir(usr.HomeDir)
	commonUserData := info.UserCommonDataDir(usr.HomeDir)
	for _, d := range []string{userData, commonUserData} {
		if err := os.MkdirAll(d, 0755); err != nil {
			// TRANSLATORS: %q is the directory whose creation failed, %v the error message
			return fmt.Errorf(i18n.G("cannot create %q: %v"), d, err)
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
		return fmt.Errorf(i18n.G("cannot find app %q in %q"), appName, snapName)
	}

	return runSnapConfine(info, app.SecurityTag(), snapApp, command, "", args)
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

	// Make sure this hook is valid for this snap. If not, don't run it. This
	// isn't an error, e.g. it will happen if a snap doesn't ship a system hook.
	if hook == nil {
		return nil
	}

	return runSnapConfine(info, hook.SecurityTag(), snapName, "", hook.Name, nil)
}

func runSnapConfine(info *snap.Info, securityTag, snapApp, command, hook string, args []string) error {
	if err := createUserDataDirs(info); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

	cmd := []string{
		"/usr/bin/ubuntu-core-launcher",
		securityTag,
		securityTag,
		"/usr/lib/snapd/snap-exec",
	}

	if command != "" {
		cmd = append(cmd, "--command="+command)
	}

	if hook != "" {
		cmd = append(cmd, "--hook="+hook)
	}

	// snap-exec is POSIXly-- options must come before positionals.
	cmd = append(cmd, snapApp)
	cmd = append(cmd, args...)

	return syscallExec(cmd[0], cmd, snapenv.ExecEnv(info))
}
