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
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/snap"
)

// for the tests
var syscallExec = syscall.Exec

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Printf("cannot snap-exec: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var opts struct {
		Positional struct {
			SnapApp string `positional-arg-name:"<app name>" description:"the snap (e.g. hello-world) or application to run (e.g. hello-world.env)"`
		} `positional-args:"yes" required:"yes"`

		Command string `long:"command" description:"alternative command to run" hidden:"yes"`
		Hook    string `long:"hook" description:"hook to run" hidden:"yes"`
	}

	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	extraArgs, err := parser.ParseArgs(args)
	if err != nil {
		return err
	}

	// Catch some invalid parameter combinations, provide helpful errors
	if opts.Hook != "" && opts.Command != "" {
		return fmt.Errorf("cannot use --hook and --command together")
	}
	if opts.Hook != "" && len(extraArgs) > 0 {
		return fmt.Errorf("too many arguments for hook %q: %s", opts.Hook, strings.Join(extraArgs, " "))
	}

	// the SNAP_REVISION is set by `snap run` - we can not (easily)
	// find it in `snap-exec` because `snap-exec` is run inside the
	// confinement and (generally) can not talk to snapd
	revision := os.Getenv("SNAP_REVISION")

	// Now actually handle the dispatching
	if opts.Hook != "" {
		return snapExecHook(opts.Positional.SnapApp, revision, opts.Hook)
	}

	return snapExecApp(opts.Positional.SnapApp, revision, opts.Command, extraArgs)
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

func snapExecApp(snapApp, revision, command string, args []string) error {
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

	// build the environment from the yaml
	env := append(os.Environ(), app.Env()...)

	// run the command
	fullCmd := filepath.Join(app.Snap.MountDir(), cmd)
	fullCmdArgs := []string{fullCmd}
	fullCmdArgs = append(fullCmdArgs, args...)
	return syscallExec(fullCmd, fullCmdArgs, env)
}

func snapExecHook(snapName, revision, hookName string) error {
	rev, err := snap.ParseRevision(revision)
	if err != nil {
		return err
	}

	info, err := snap.ReadInfo(snapName, &snap.SideInfo{
		Revision: rev,
	})
	if err != nil {
		return err
	}

	hook := info.Hooks[hookName]
	if hook == nil {
		return fmt.Errorf("cannot find hook %q in %q", hookName, snapName)
	}

	// build the environment
	env := append(os.Environ(), hook.Env()...)

	// run the hook
	hookPath := filepath.Join(hook.Snap.HooksDir(), hook.Name)
	return syscallExec(hookPath, []string{hookPath}, env)
}
