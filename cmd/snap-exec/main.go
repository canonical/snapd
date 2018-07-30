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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
)

// for the tests
var syscallExec = syscall.Exec

// commandline args
var opts struct {
	Command          string `long:"command" description:"use a different command like {stop,post-stop} from the app"`
	SkipCommandChain bool   `long:"skip-command-chain" description:"don't run command chain"`
	Hook             string `long:"hook" description:"hook to run" hidden:"yes"`
}

func init() {
	// plug/slot sanitization not used nor possible from snap-exec, make it no-op
	snap.SanitizePlugsSlots = func(snapInfo *snap.Info) {}
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

	return execApp(snapApp, revision, opts.Command, extraArgs, opts.SkipCommandChain)
}

const defaultShell = "/bin/bash"

func findCommand(app *snap.AppInfo, command string, skipCommandChain bool) (string, error) {
	var chain []string

	if !skipCommandChain {
		for _, element := range app.CommandChain {
			chain = append(chain, filepath.Join(app.Snap.MountDir(), element))
		}
	}

	switch command {
	case "shell":
		chain = append(chain, defaultShell)
	case "complete":
		if app.Completer != "" {
			chain = append(chain, defaultShell)
		}
	case "stop":
		if app.StopCommand != "" {
			chain = append(chain, filepath.Join(app.Snap.MountDir(), app.StopCommand))
		}
	case "reload":
		if app.ReloadCommand != "" {
			chain = append(chain, filepath.Join(app.Snap.MountDir(), app.ReloadCommand))
		}
	case "post-stop":
		if app.PostStopCommand != "" {
			chain = append(chain, filepath.Join(app.Snap.MountDir(), app.PostStopCommand))
		}
	case "", "gdb":
		chain = append(chain, filepath.Join(app.Snap.MountDir(), app.Command))
	default:
		return "", fmt.Errorf("cannot use %q command", command)
	}

	if len(chain) == len(app.CommandChain) {
		return "", fmt.Errorf("no %q command found for %q", command, app.Name)
	}
	return strings.Join(chain, " "), nil
}

// expandEnvCmdArgs takes the string list of commandline arguments
// and expands any $VAR with the given var from the env argument.
func expandEnvCmdArgs(args []string, env map[string]string) []string {
	cmdArgs := make([]string, 0, len(args))
	for _, arg := range args {
		maybeExpanded := os.Expand(arg, func(k string) string {
			return env[k]
		})
		if maybeExpanded != "" {
			cmdArgs = append(cmdArgs, maybeExpanded)
		}
	}
	return cmdArgs
}

func execApp(snapApp, revision, command string, args []string, skipCommandChain bool) error {
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

	cmdAndArgs, err := findCommand(app, command, skipCommandChain)
	if err != nil {
		return err
	}

	// build the environment from the yaml, translating TMPDIR and
	// similar variables back from where they were hidden when
	// invoking the setuid snap-confine.
	env := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, snapenv.PreservedUnsafePrefix) {
			kv = kv[len(snapenv.PreservedUnsafePrefix):]
		}
		env = append(env, kv)
	}
	env = append(env, osutil.SubstituteEnv(app.Env())...)

	// strings.Split() is ok here because we validate all app fields and the
	// whitelist is pretty strict (see snap/validate.go:appContentWhitelist)
	// (see also snap/container.go's normPath). No quotes are
	// supported, so whitespace is actually a delimiter.
	cmd := strings.Split(cmdAndArgs, " ")
	cmd = expandEnvCmdArgs(cmd, osutil.EnvMap(env))

	var extraArgs []string
	switch command {
	case "complete":
		extraArgs = append(extraArgs, dirs.CompletionHelper, filepath.Join(app.Snap.MountDir(), app.Completer))
	case "gdb":
		cmd = append([]string{filepath.Join(dirs.CoreLibExecDir, "snap-gdb-shim")}, cmd...)
	}
	cmd = append(cmd, extraArgs...)
	cmd = append(cmd, args...)
	if err := syscallExec(cmd[0], cmd, env); err != nil {
		return fmt.Errorf("cannot exec %q: %s", cmd[0], err)
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
	env := append(os.Environ(), osutil.SubstituteEnv(hook.Env())...)

	// run the hook
	hookPath := filepath.Join(hook.Snap.HooksDir(), hook.Name)
	return syscallExec(hookPath, []string{hookPath}, env)
}
