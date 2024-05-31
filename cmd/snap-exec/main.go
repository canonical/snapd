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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
)

// for the tests
var syscallExec = syscall.Exec
var syscallStat = syscall.Stat
var osReadlink = os.Readlink

// commandline args
var opts struct {
	Command string `long:"command" description:"use a different command like {stop,post-stop} from the app"`
	Hook    string `long:"hook" description:"hook to run" hidden:"yes"`
}

func init() {
	// plug/slot sanitization not used nor possible from snap-exec, make it no-op
	snap.SanitizePlugsSlots = func(snapInfo *snap.Info) {}
	logger.SimpleSetup(nil)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot snap-exec: %s\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (app string, appArgs []string, err error) {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	rest, err := parser.ParseArgs(args)
	if err != nil {
		return "", nil, err
	}
	if len(rest) == 0 {
		return "", nil, fmt.Errorf("need the application to run as argument")
	}

	// Catch some invalid parameter combinations, provide helpful errors
	if opts.Hook != "" && opts.Command != "" {
		return "", nil, fmt.Errorf("cannot use --hook and --command together")
	}
	if opts.Hook != "" && len(rest) > 1 {
		return "", nil, fmt.Errorf("too many arguments for hook %q: %s", opts.Hook, strings.Join(rest, " "))
	}

	return rest[0], rest[1:], nil
}

func run() error {
	snapApp, extraArgs, err := parseArgs(os.Args[1:])
	if err != nil {
		return err
	}

	// the SNAP_REVISION is set by `snap run` - we can not (easily)
	// find it in `snap-exec` because `snap-exec` is run inside the
	// confinement and (generally) can not talk to snapd
	revision := os.Getenv("SNAP_REVISION")

	// Now actually handle the dispatching
	if opts.Hook != "" {
		return execHook(snapApp, revision, opts.Hook)
	}

	return execApp(snapApp, revision, opts.Command, extraArgs)
}

const defaultShell = "/bin/bash"

func findCommand(app *snap.AppInfo, command string) (string, error) {
	var cmd string
	switch command {
	case "shell":
		cmd = defaultShell
	case "complete":
		if app.Completer != "" {
			cmd = defaultShell
		}
	case "stop":
		cmd = app.StopCommand
	case "reload":
		cmd = app.ReloadCommand
	case "post-stop":
		cmd = app.PostStopCommand
	case "", "gdb", "gdbserver":
		cmd = app.Command
	default:
		return "", fmt.Errorf("cannot use %q command", command)
	}

	if cmd == "" {
		return "", fmt.Errorf("no %q command found for %q", command, app.Name)
	}
	return cmd, nil
}

func absoluteCommandChain(snapInfo *snap.Info, commandChain []string) []string {
	chain := make([]string, 0, len(commandChain))
	snapMountDir := snapInfo.MountDir()

	for _, element := range commandChain {
		chain = append(chain, filepath.Join(snapMountDir, element))
	}

	return chain
}

// expandEnvCmdArgs takes the string list of commandline arguments
// and expands any $VAR with the given var from the env argument.
func expandEnvCmdArgs(args []string, env osutil.Environment) []string {
	cmdArgs := make([]string, 0, len(args))
	for _, arg := range args {
		maybeExpanded := os.Expand(arg, func(varName string) string {
			return env[varName]
		})
		if maybeExpanded != "" {
			cmdArgs = append(cmdArgs, maybeExpanded)
		}
	}
	return cmdArgs
}

func completionHelper() (string, error) {
	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "etelpmoc.sh"), nil
}

func execApp(snapApp, revision, command string, args []string) error {
	rev, err := snap.ParseRevision(revision)
	if err != nil {
		return fmt.Errorf("cannot parse revision %q: %s", revision, err)
	}

	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := snap.ReadInfo(snapName, &snap.SideInfo{
		Revision: rev,
	})
	if err != nil {
		return fmt.Errorf("cannot read info for %q: %s", snapName, err)
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf("cannot find app %q in %q", appName, snapName)
	}

	cmdAndArgs, err := findCommand(app, command)
	if err != nil {
		return err
	}

	// build the environment from the yaml, translating TMPDIR and
	// similar variables back from where they were hidden when
	// invoking the setuid snap-confine.
	env, err := osutil.OSEnvironmentUnescapeUnsafe(snapenv.PreservedUnsafePrefix)
	if err != nil {
		return err
	}
	for _, eenv := range app.EnvChain() {
		env.ExtendWithExpanded(eenv)
	}

	// this is a workaround for the lack of an environment backend in interfaces
	// where we want certain interfaces when connected to add environment
	// variables to plugging snap apps, but this is a lot simpler as a
	// work-around
	// we currently only handle the CUPS_SERVER environment variable, setting it
	// to /var/cups/ if that dir is a bind-mount - it should not be one
	// except in a strictly confined snap where we setup the bind mount from the
	// source cups slot snap to the plugging snap.
	var stVar, stVarCups syscall.Stat_t
	err1 := syscallStat(dirs.GlobalRootDir+"/var/", &stVar)
	err2 := syscallStat(dirs.GlobalRootDir+"/var/cups/", &stVarCups)
	if err1 == nil && err2 == nil && stVar.Dev != stVarCups.Dev {
		env["CUPS_SERVER"] = "/var/cups/cups.sock"
	}

	// strings.Split() is ok here because we validate all app fields and the
	// whitelist is pretty strict (see snap/validate.go:appContentWhitelist)
	// (see also overlord/snapstate/check_snap.go's normPath)
	tmpArgv := strings.Split(cmdAndArgs, " ")
	cmd := tmpArgv[0]
	cmdArgs := expandEnvCmdArgs(tmpArgv[1:], env)

	// run the command
	fullCmd := []string{filepath.Join(app.Snap.MountDir(), cmd)}
	switch command {
	case "shell":
		fullCmd[0] = defaultShell
		cmdArgs = nil
	case "complete":
		fullCmd[0] = defaultShell
		helper, err := completionHelper()
		if err != nil {
			return fmt.Errorf("cannot find completion helper: %v", err)
		}
		cmdArgs = []string{
			helper,
			filepath.Join(app.Snap.MountDir(), app.Completer),
		}
	case "gdb":
		fullCmd = append(fullCmd, fullCmd[0])
		fullCmd[0] = filepath.Join(dirs.CoreLibExecDir, "snap-gdb-shim")
	case "gdbserver":
		fullCmd = append(fullCmd, fullCmd[0])
		fullCmd[0] = filepath.Join(dirs.CoreLibExecDir, "snap-gdbserver-shim")
	}
	fullCmd = append(fullCmd, cmdArgs...)
	fullCmd = append(fullCmd, args...)

	fullCmd = append(absoluteCommandChain(app.Snap, app.CommandChain), fullCmd...)

	logger.StartupStageTimestamp("snap-exec to app")
	if err := syscallExec(fullCmd[0], fullCmd, env.ForExec()); err != nil {
		return fmt.Errorf("cannot exec %q: %s", fullCmd[0], err)
	}
	// this is never reached except in tests
	return nil
}

func execHook(snapName, revision, hookName string) error {
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
	// NOTE: we do not use OSEnvironmentUnescapeUnsafe, we do not
	// particurly want to transmit snapd exec environment details
	// to the hooks
	env, err := osutil.OSEnvironment()
	if err != nil {
		return err
	}
	for _, eenv := range hook.EnvChain() {
		env.ExtendWithExpanded(eenv)
	}

	// run the hook
	cmd := append(absoluteCommandChain(hook.Snap, hook.CommandChain), filepath.Join(hook.Snap.HooksDir(), hook.Name))
	return syscallExec(cmd[0], cmd, env.ForExec())
}
