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

// commandline args
var opts struct {
	Command string `long:"command" description:"use a different command like {stop,post-stop} from the app"`
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("cannot snap-exec: %s\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) ([]string, error) {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	rest, err := parser.ParseArgs(args)
	if err != nil {
		return nil, err
	}
	if len(rest) == 0 {
		return nil, fmt.Errorf("need the application to run as argument")
	}

	return rest, nil
}

func run() error {
	args, err := parseArgs(os.Args)
	if err != nil {
		return err
	}

	// the SNAP_REVISION is set by `snap run` - we can not (easily)
	// find it in `snap-exec` because `snap-exec` is run inside the
	// confinement and (generally) can not talk to snapd
	revision := os.Getenv("SNAP_REVISION")

	snapApp := args[0]
	return snapExec(snapApp, revision, opts.Command, args[1:])
}

func findCommand(app *snap.AppInfo, command string) (string, error) {
	var cmd string
	switch command {
	case "shell":
		cmd = "/bin/bash"
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
	fullCmdArgs := []string{fullCmd}
	fullCmdArgs = append(fullCmdArgs, args...)
	return syscallExec(fullCmd, fullCmdArgs, env)
}
