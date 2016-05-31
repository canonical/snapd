// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"github.com/snapcore/snapd/snap"
)

// for the tests
var syscallExec = syscall.Exec

func main() {
	if err := run(); err != nil {
		fmt.Printf("cannot snap-exec: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	var opts struct {
		Positional struct {
			SnapApp string `positional-arg-name:"<snapApp>" description:"the application to run, e.g. hello-world.env"`
		} `positional-args:"yes" required:"yes"`

		Command string `long:"command" description:"use a different command like {stop,post-stop} from the app"`
	}

	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	args, err := parser.Parse()
	if err != nil {
		return err
	}

	// the SNAP_REVISION is set by `snap run` - we can not (easily)
	// find it in `snap-exec` because `snap-exec` is run inside the
	// confinement and (generally) can not talk to snapd
	revision := os.Getenv("SNAP_REVISION")

	snapApp := opts.Positional.SnapApp
	return snapExec(snapApp, revision, opts.Command, args)
}

func findCommand(app *snap.AppInfo, command string) (string, error) {
	var cmd string
	switch command {
	case "stop":
		cmd = app.StopCommand
	case "post-stop":
		cmd = app.PostStopCommand
	case "":
		cmd = app.Command
	default:
		return "", fmt.Errorf("cannot use %q command", command)
	}

	if cmd == "" {
		return "", fmt.Errorf("no %q command found for %q", command, app.Name)
	}
	return cmd, nil
}

func snapExec(snapApp, revision, command string, args []string) error {
	rev, err := snap.ParseRevision(revision)
	if err != nil {
		return err
	}

	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := snap.ReadInfo(snapName, &snap.SideInfo{
		Revision: rev,
	})
	if err != nil {
		return err
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf("cannot find app %q in %q", appName, snapName)
	}

	cmd, err := findCommand(app, command)
	if err != nil {
		return err
	}

	// build the evnironment from the yamle
	env := append(os.Environ(), app.Env()...)

	// run the command
	fullCmd := filepath.Join(app.Snap.MountDir(), cmd)
	return syscallExec(fullCmd, args, env)
}
