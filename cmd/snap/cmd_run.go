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
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
	"github.com/snapcore/snapd/x11"
)

var (
	syscallExec = syscall.Exec
	userCurrent = user.Current
	osGetenv    = os.Getenv
)

type cmdRun struct {
	Command  string `long:"command" hidden:"yes"`
	Hook     string `long:"hook" hidden:"yes"`
	Revision string `short:"r" default:"unset" hidden:"yes"`
	Shell    bool   `long:"shell" `
	// FIXME: provide a way to pass options to strace
	Strace bool `long:"strace"`
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
			"strace":  i18n.G("Run the command under strace (useful for debugging"),
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
	switch {
	case x.Shell:
		x.Command = "shell"
	case x.Strace:
		x.Command = "strace"
	}

	if x.Command == "complete" {
		snapApp, args = antialias(snapApp, args)
	}

	return snapRunApp(snapApp, x.Command, args)
}

// antialias changes snapApp and args if snapApp is actually an alias
// for something else. If not, or if the args aren't what's expected
// for completion, it returns them unchanged.
func antialias(snapApp string, args []string) (string, []string) {
	if len(args) < 7 {
		// NOTE if len(args) < 7, Something is Wrong (at least WRT complete.sh and etelpmoc.sh)
		return snapApp, args
	}

	actualApp, err := resolveApp(snapApp)
	if err != nil || actualApp == snapApp {
		// no alias! woop.
		return snapApp, args
	}

	compPoint, err := strconv.Atoi(args[2])
	if err != nil {
		// args[2] is not COMP_POINT
		return snapApp, args
	}

	if compPoint <= len(snapApp) {
		// COMP_POINT is inside $0
		return snapApp, args
	}

	if compPoint > len(args[5]) {
		// COMP_POINT is bigger than $#
		return snapApp, args
	}

	if args[6] != snapApp {
		// args[6] is not COMP_WORDS[0]
		return snapApp, args
	}

	// it _should_ be COMP_LINE followed by one of
	// COMP_WORDBREAKS, but that's hard to do
	re, err := regexp.Compile(`^` + regexp.QuoteMeta(snapApp) + `\b`)
	if err != nil || !re.MatchString(args[5]) {
		// (weird regexp error, or) args[5] is not COMP_LINE
		return snapApp, args
	}

	argsOut := make([]string, len(args))
	copy(argsOut, args)

	argsOut[2] = strconv.Itoa(compPoint - len(snapApp) + len(actualApp))
	argsOut[5] = re.ReplaceAllLiteralString(args[5], actualApp)
	argsOut[6] = actualApp

	return actualApp, argsOut
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

func migrateXauthority(info *snap.Info) (string, error) {
	u, err := userCurrent()
	if err != nil {
		return "", fmt.Errorf(i18n.G("cannot get the current user: %s"), err)
	}

	// If our target directory (XDG_RUNTIME_DIR) doesn't exist we
	// don't attempt to create it.
	baseTargetDir := filepath.Join(dirs.XdgRuntimeDirBase, u.Uid)
	if !osutil.FileExists(baseTargetDir) {
		return "", nil
	}

	xauthPath := osGetenv("XAUTHORITY")
	if len(xauthPath) == 0 || !osutil.FileExists(xauthPath) {
		// Nothing to do for us. Most likely running outside of any
		// graphical X11 session.
		return "", nil
	}

	fin, err := os.Open(xauthPath)
	if err != nil {
		return "", err
	}
	defer fin.Close()

	// Abs() also calls Clean(); see https://golang.org/pkg/path/filepath/#Abs
	xauthPathAbs, err := filepath.Abs(fin.Name())
	if err != nil {
		return "", nil
	}

	// Remove all symlinks from path
	xauthPathCan, err := filepath.EvalSymlinks(xauthPathAbs)
	if err != nil {
		return "", nil
	}

	// Ensure the XAUTHORITY env is not abused by checking that
	// it point to exactly the file we just opened (no symlinks,
	// no funny "../.." etc)
	if fin.Name() != xauthPathCan {
		logger.Noticef("WARNING: XAUTHORITY environment value is not a clean path: %q", xauthPathCan)
		return "", nil
	}

	// Only do the migration from /tmp since the real /tmp is not visible for snaps
	if !strings.HasPrefix(fin.Name(), "/tmp/") {
		return "", nil
	}

	// We are performing a Stat() here to make sure that the user can't
	// steal another user's Xauthority file. Note that while Stat() uses
	// fstat() on the file descriptor created during Open(), the file might
	// have changed ownership between the Open() and the Stat(). That's ok
	// because we aren't trying to block access that the user already has:
	// if the user has the privileges to chown another user's Xauthority
	// file, we won't block that since the user can just steal it without
	// having to use snap run. This code is just to ensure that a user who
	// doesn't have those privileges can't steal the file via snap run
	// (also note that the (potentially untrusted) snap isn't running yet).
	fi, err := fin.Stat()
	if err != nil {
		return "", err
	}
	sys := fi.Sys()
	if sys == nil {
		return "", fmt.Errorf(i18n.G("cannot validate owner of file %s"), fin.Name())
	}
	// cheap comparison as the current uid is only available as a string
	// but it is better to convert the uid from the stat result to a
	// string than a string into a number.
	if fmt.Sprintf("%d", sys.(*syscall.Stat_t).Uid) != u.Uid {
		return "", fmt.Errorf(i18n.G("Xauthority file isn't owned by the current user %s"), u.Uid)
	}

	targetPath := filepath.Join(baseTargetDir, ".Xauthority")

	// Only validate Xauthority file again when both files don't match
	// otherwise we can continue using the existing Xauthority file.
	// This is ok to do here because we aren't trying to protect against
	// the user changing the Xauthority file in XDG_RUNTIME_DIR outside
	// of snapd.
	if osutil.FileExists(targetPath) {
		var fout *os.File
		if fout, err = os.Open(targetPath); err != nil {
			return "", err
		}
		if osutil.StreamsEqual(fin, fout) {
			fout.Close()
			return targetPath, nil
		}

		fout.Close()
		if err := os.Remove(targetPath); err != nil {
			return "", err
		}

		// Ensure we're validating the Xauthority file from the beginning
		if _, err := fin.Seek(int64(os.SEEK_SET), 0); err != nil {
			return "", err
		}
	}

	// To guard against setting XAUTHORITY to non-xauth files, check
	// that we have a valid Xauthority. Specifically, the file must be
	// parseable as an Xauthority file and not be empty.
	if err := x11.ValidateXauthority(fin); err != nil {
		return "", err
	}

	// Read data from the beginning of the file
	if _, err = fin.Seek(int64(os.SEEK_SET), 0); err != nil {
		return "", err
	}

	fout, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer fout.Close()

	// Read and write validated Xauthority file to its right location
	if _, err = io.Copy(fout, fin); err != nil {
		if err := os.Remove(targetPath); err != nil {
			logger.Noticef("WARNING: cannot remove file at %s: %s", targetPath, err)
		}
		return "", fmt.Errorf(i18n.G("cannot write new Xauthority file at %s: %s"), targetPath, err)
	}

	return targetPath, nil
}

func straceCmd() ([]string, error) {
	current, err := user.Current()
	if err != nil {
		return nil, err
	}
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("cannot use strace without sudo: %s", err)
	}
	stracePath, err := exec.LookPath("strace")
	if err != nil {
		// Ubuntu Core devices will need strace from the snap
		cand := filepath.Join(dirs.SnapMountDir, "strace-static", "current", "bin", "strace")
		if osutil.FileExists(cand) {
			stracePath = cand
		} else {
			return nil, err
		}
	}

	return []string{
		sudoPath, "-E",
		stracePath,
		"-u", current.Username,
		"-f",
		"-e", "!select,pselect6,_newselect,clock_gettime",
	}, nil
}

func runSnapConfine(info *snap.Info, securityTag, snapApp, command, hook string, args []string) error {
	snapConfine := filepath.Join(dirs.DistroLibExecDir, "snap-confine")
	// if we re-exec, we must run the snap-confine from the core snap
	// as well, if they get out of sync, havoc will happen
	if isReexeced() {
		// run snap-confine from the core snap. that will work because
		// snap-confine on the core snap is mostly statically linked
		// (except libudev and libc)
		snapConfine = filepath.Join(dirs.SnapMountDir, "core/current", dirs.CoreLibExecDir, "snap-confine")
	}

	if !osutil.FileExists(snapConfine) {
		if hook != "" {
			logger.Noticef("WARNING: skipping running hook %q of snap %q: missing snap-confine", hook, info.Name())
			return nil
		}
		return fmt.Errorf(i18n.G("missing snap-confine: try updating your snapd package"))
	}

	if err := createUserDataDirs(info); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

	xauthPath, err := migrateXauthority(info)
	if err != nil {
		logger.Noticef("WARNING: cannot copy user Xauthority file: %s", err)
	}

	var cmd []string

	if command == "strace" {
		strace, err := straceCmd()
		if err != nil {
			return err
		}
		cmd = append(cmd, strace...)
		command = ""
	}
	cmd = append(cmd, snapConfine)

	if info.NeedsClassic() {
		cmd = append(cmd, "--classic")
	}
	if info.Base != "" {
		cmd = append(cmd, "--base", info.Base)
	}
	cmd = append(cmd, securityTag)

	// when under confinement, snap-exec is run from 'core' snap rootfs
	snapExecPath := filepath.Join(dirs.CoreLibExecDir, "snap-exec")

	if info.NeedsClassic() {
		// running with classic confinement, carefully pick snap-exec we
		// are going to use
		if isReexeced() {
			// same rule as when choosing the location of snap-confine
			snapExecPath = filepath.Join(dirs.SnapMountDir, "core/current",
				dirs.CoreLibExecDir, "snap-exec")
		} else {
			// there is no mount namespace where 'core' is the
			// rootfs, hence we need to use distro's snap-exec
			snapExecPath = filepath.Join(dirs.DistroLibExecDir, "snap-exec")
		}
	}
	cmd = append(cmd, snapExecPath)

	if command != "" {
		cmd = append(cmd, "--command="+command)
	}

	if hook != "" {
		cmd = append(cmd, "--hook="+hook)
	}

	// snap-exec is POSIXly-- options must come before positionals.
	cmd = append(cmd, snapApp)
	cmd = append(cmd, args...)

	extraEnv := make(map[string]string)
	if len(xauthPath) > 0 {
		extraEnv["XAUTHORITY"] = xauthPath
	}
	env := snapenv.ExecEnv(info, extraEnv)

	return syscallExec(cmd[0], cmd, env)
}
