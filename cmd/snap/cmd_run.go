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
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
)

type cmdRun struct {
	Positional struct {
		SnapApp string `positional-arg-name:"<app name>" description:"the application to run, e.g. hello-world.env"`
	} `positional-args:"yes" required:"yes"`

	Command string `long:"command" description:"alternative command to run"`
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
	return snapRun(x.Positional.SnapApp, x.Command, args)
}

func getSnapInfo(snapName string) (*snap.Info, error) {
	// we need to get the revision here because once we are inside
	// the confinement the snapd API may be unavailable.
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
	sn := snaps[0]
	info, err := snap.ReadInfo(snapName, &snap.SideInfo{
		Revision: snap.R(sn.Revision.N),
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}

// returns the app environment that is important for
// the later stages of executing the application
// (like SNAP_REVISION that snap-exec requires to work)
func snapExecAppEnv(app *snap.AppInfo) []string {
	env := snapenv.Basic(app.Snap)
	env = append(env, snapenv.User(app.Snap, os.Getenv("HOME"))...)
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

var syscallExec = syscall.Exec
var userCurrent = user.Current

func snapRun(snapApp, command string, args []string) error {
	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := getSnapInfo(snapName)
	if err != nil {
		return err
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf("cannot find app %q in %q", appName, snapName)
	}

	if err := createUserDataDirs(info); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

	// build command to run
	cmd := []string{
		"/usr/bin/ubuntu-core-launcher",
		app.SecurityTag(),
		app.SecurityTag(),
		"/usr/lib/snapd/snap-exec",
		snapApp,
	}
	if command != "" {
		cmd = append(cmd, "--command="+command)
	}
	cmd = append(cmd, args...)

	// build env
	env := append(os.Environ(), snapExecAppEnv(app)...)

	// launch!
	return syscallExec(cmd[0], cmd, env)
}
