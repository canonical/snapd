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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
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
		curFn := filepath.Join(dirs.SnapMountDir, snapName, "current")
		realFn, err := os.Readlink(curFn)
		if err != nil {
			return nil, fmt.Errorf("cannot find current revision for snap %s: %s", snapName, err)
		}
		rev := filepath.Base(realFn)
		revision, err = snap.ParseRevision(rev)
		if err != nil {
			return nil, fmt.Errorf("cannot read revision %s: %s", rev, err)
		}
	}

	info, err := snap.ReadInfo(snapName, &snap.SideInfo{
		Revision: revision,
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}

func createOrUpdateUserDataSymlink(info *snap.Info, usr *user.User) error {
	// 'current' symlink for user data (SNAP_USER_DATA)
	userData := info.UserDataDir(usr.HomeDir)
	wantedSymlinkValue := filepath.Base(userData)
	currentActiveSymlink := filepath.Join(userData, "..", "current")

	var err error
	var currentSymlinkValue string
	for i := 0; i < 5; i++ {
		currentSymlinkValue, err = os.Readlink(currentActiveSymlink)
		// Failure other than non-existing symlink is fatal
		if err != nil && !os.IsNotExist(err) {
			// TRANSLATORS: %v the error message
			return fmt.Errorf(i18n.G("cannot read symlink: %v"), err)
		}

		if currentSymlinkValue == wantedSymlinkValue {
			break
		}

		if err == nil {
			// We may be racing with other instances of snap-run that try to do the same thing
			// If the symlink is already removed then we can ignore this error.
			err = os.Remove(currentActiveSymlink)
			if err != nil && !os.IsNotExist(err) {
				// abort with error
				break
			}
		}

		err = os.Symlink(wantedSymlinkValue, currentActiveSymlink)
		// Error other than symlink already exists will abort and be propagated
		if err == nil || !os.IsExist(err) {
			break
		}
		// If we arrived here it means the symlink couldn't be created because it got created
		// in the meantime by another instance, so we will try again.
	}
	if err != nil {
		return fmt.Errorf(i18n.G("cannot update the 'current' symlink of %q: %v"), currentActiveSymlink, err)
	}
	return nil
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

	return createOrUpdateUserDataSymlink(info, usr)
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
	if hook == nil {
		return fmt.Errorf(i18n.G("cannot find hook %q in %q"), hookName, snapName)
	}

	return runSnapConfine(info, hook.SecurityTag(), snapName, "", hook.Name, nil)
}

var osReadlink = os.Readlink

func isReexeced() bool {
	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v", err)
		return false
	}
	return strings.HasPrefix(exe, dirs.SnapMountDir)
}

func runSnapConfine(info *snap.Info, securityTag, snapApp, command, hook string, args []string) error {
	if err := createUserDataDirs(info); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

	cmd := []string{filepath.Join(dirs.DistroLibExecDir, "snap-confine")}
	if info.NeedsClassic() {
		cmd = append(cmd, "--classic")
	}
	cmd = append(cmd, securityTag)
	cmd = append(cmd, filepath.Join(dirs.CoreLibExecDir, "snap-exec"))

	if command != "" {
		cmd = append(cmd, "--command="+command)
	}

	if hook != "" {
		cmd = append(cmd, "--hook="+hook)
	}

	// snap-exec is POSIXly-- options must come before positionals.
	cmd = append(cmd, snapApp)
	cmd = append(cmd, args...)

	// FIXME2: for now "ForceDevMode" will prevent snap-confine to be
	//         run on distros like fedora/suse etc. which is ok because
	//         snap-confine is a non-op on these distros anyway.
	if isReexeced() && !release.ReleaseInfo.ForceDevMode() {
		// run snap-confine from the core snap. that will work because
		// snap-confine on the core snap is mostly statically linked
		// (except libudev and libc)
		cmd[0] = filepath.Join(dirs.SnapMountDir, "core/current", cmd[0])
	}

	return syscallExec(cmd[0], cmd, snapenv.ExecEnv(info))
}
