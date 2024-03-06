// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/desktop/portal"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/strace"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/snapenv"
	"github.com/snapcore/snapd/strutil/shlex"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/x11"
)

var (
	syscallExec              = syscall.Exec
	userCurrent              = user.Current
	osGetenv                 = os.Getenv
	timeNow                  = time.Now
	selinuxIsEnabled         = selinux.IsEnabled
	selinuxVerifyPathContext = selinux.VerifyPathContext
	selinuxRestoreContext    = selinux.RestoreContext
)

type cmdRun struct {
	clientMixin
	Command  string `long:"command" hidden:"yes"`
	HookName string `long:"hook" hidden:"yes"`
	Revision string `short:"r" default:"unset" hidden:"yes"`
	Shell    bool   `long:"shell" `
	DebugLog bool   `long:"debug-log"`

	// This options is both a selector (use or don't use strace) and it
	// can also carry extra options for strace. This is why there is
	// "default" and "optional-value" to distinguish this.
	Strace string `long:"strace" optional:"true" optional-value:"with-strace" default:"no-strace" default-mask:"-"`
	// deprecated in favor of Gdbserver
	Gdb                   bool   `long:"gdb" hidden:"yes"`
	Gdbserver             string `long:"gdbserver" default:"no-gdbserver" optional-value:":0" optional:"true"`
	ExperimentalGdbserver string `long:"experimental-gdbserver" default:"no-gdbserver" optional-value:":0" optional:"true" hidden:"yes"`
	TraceExec             bool   `long:"trace-exec"`

	// not a real option, used to check if cmdRun is initialized by
	// the parser
	ParserRan int    `long:"parser-ran" default:"1" hidden:"yes"`
	Timer     string `long:"timer" hidden:"yes"`
}

func init() {
	addCommand("run",
		i18n.G("Run the given snap command"),
		i18n.G(`
The run command executes the given snap command with the right confinement
and environment.
`),
		func() flags.Commander {
			return &cmdRun{}
		}, map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"command": i18n.G("Alternative command to run"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"hook": i18n.G("Hook to run"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"r": i18n.G("Use a specific snap revision when running hook"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"shell": i18n.G("Run a shell instead of the command (useful for debugging)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"strace": i18n.G("Run the command under strace (useful for debugging). Extra strace options can be specified as well here. Pass --raw to strace early snap helpers."),
			// TRANSLATORS: This should not start with a lowercase letter.
			"gdb": i18n.G("Run the command with gdb (deprecated, use --gdbserver instead)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"gdbserver":              i18n.G("Run the command with gdbserver"),
			"experimental-gdbserver": "",
			// TRANSLATORS: This should not start with a lowercase letter.
			"timer": i18n.G("Run as a timer service with given schedule"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"trace-exec": i18n.G("Display exec calls timing data"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"debug-log":  i18n.G("Enable debug logging during early snap startup phases"),
			"parser-ran": "",
		}, nil)
}

// isStopping returns true if the system is shutting down.
func isStopping() (bool, error) {
	// Make sure, just in case, that systemd doesn't localize the output string.
	env, err := osutil.OSEnvironment()
	if err != nil {
		return false, err
	}
	env["LC_MESSAGES"] = "C"
	// Check if systemd is stopping (shutting down or rebooting).
	cmd := exec.Command("systemctl", "is-system-running")
	cmd.Env = env.ForExec()
	stdout, err := cmd.Output()
	// systemctl is-system-running returns non-zero for outcomes other than "running"
	// As such, ignore any ExitError and just process the stdout buffer.
	if _, ok := err.(*exec.ExitError); ok {
		return string(stdout) == "stopping\n", nil
	}
	return false, err
}

func maybeWaitForSecurityProfileRegeneration(cli *client.Client) error {
	// check if the security profiles key has changed, if so, we need
	// to wait for snapd to re-generate all profiles
	mismatch, err := interfaces.SystemKeyMismatch()
	if err == nil && !mismatch {
		return nil
	}
	// something went wrong with the system-key compare, try to
	// reach snapd before continuing
	if err != nil {
		logger.Debugf("SystemKeyMismatch returned an error: %v", err)
	}

	// We have a mismatch but maybe it is only because systemd is shutting down
	// and core or snapd were already unmounted and we failed to re-execute.
	// For context see: https://bugs.launchpad.net/snapd/+bug/1871652
	stopping, err := isStopping()
	if err != nil {
		logger.Debugf("cannot check if system is stopping: %s", err)
	}
	if stopping {
		logger.Debugf("ignoring system key mismatch during system shutdown/reboot")
		return nil
	}

	// We have a mismatch, try to connect to snapd, once we can
	// connect we just continue because that usually means that
	// a new snapd is ready and has generated profiles.
	//
	// There is a corner case if an upgrade leaves the old snapd
	// running and we connect to the old snapd. Handling this
	// correctly is tricky because our "snap run" pipeline may
	// depend on profiles written by the new snapd. So for now we
	// just continue and hope for the best. The real fix for this
	// is to fix the packaging so that snapd is stopped, upgraded
	// and started.
	//
	// connect timeout for client is 5s on each try, so 12*5s = 60s
	timeout := 12
	if timeoutEnv := os.Getenv("SNAPD_DEBUG_SYSTEM_KEY_RETRY"); timeoutEnv != "" {
		if i, err := strconv.Atoi(timeoutEnv); err == nil {
			timeout = i
		}
	}

	logger.Debugf("system key mismatch detected, waiting for snapd to start responding...")

	for i := 0; i < timeout; i++ {
		// TODO: we could also check cli.Maintenance() here too in case snapd is
		// down semi-permanently for a refresh, but what message do we show to
		// the user or what do we do if we know snapd is down for maintenance?
		if _, err := cli.SysInfo(); err == nil {
			return nil
		}
		// sleep a little bit for good measure
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for snap system profiles to get updated")
}

func (x *cmdRun) Usage() string {
	return "[run-OPTIONS] <NAME-OF-SNAP>.<NAME-OF-APP> [<SNAP-APP-ARG>...]"
}

func (x *cmdRun) Execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(i18n.G("need the application to run as argument"))
	}
	snapApp := args[0]
	args = args[1:]

	// Catch some invalid parameter combinations, provide helpful errors
	optionsSet := 0
	for _, param := range []string{x.HookName, x.Command, x.Timer} {
		if param != "" {
			optionsSet++
		}
	}
	if optionsSet > 1 {
		return fmt.Errorf("you can only use one of --hook, --command, and --timer")
	}

	if x.Revision != "unset" && x.Revision != "" && x.HookName == "" {
		return fmt.Errorf(i18n.G("-r can only be used with --hook"))
	}
	if x.HookName != "" && len(args) > 0 {
		// TRANSLATORS: %q is the hook name; %s a space-separated list of extra arguments
		return fmt.Errorf(i18n.G("too many arguments for hook %q: %s"), x.HookName, strings.Join(args, " "))
	}

	logger.StartupStageTimestamp("start")

	if err := maybeWaitForSecurityProfileRegeneration(x.client); err != nil {
		return err
	}

	// Now actually handle the dispatching
	if x.HookName != "" {
		return x.snapRunHook(snapApp)
	}

	if x.Command == "complete" {
		snapApp, args = antialias(snapApp, args)
	}

	if x.Timer != "" {
		return x.snapRunTimer(snapApp, x.Timer, args)
	}

	return x.snapRunApp(snapApp, args)
}

func maybeWaitWhileInhibited(ctx context.Context, snapName string) error {
	// If the snap is inhibited from being used then postpone running it until
	// that condition passes. Inhibition UI can be dismissed by the user, in
	// which case we don't run the application at all.
	if features.RefreshAppAwareness.IsEnabled() {
		return waitWhileInhibited(ctx, snapName)
	}
	return nil
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

func getSnapInfo(snapName string, revision snap.Revision) (info *snap.Info, err error) {
	if revision.Unset() {
		info, err = snap.ReadCurrentInfo(snapName)
	} else {
		info, err = snap.ReadInfo(snapName, &snap.SideInfo{
			Revision: revision,
		})
	}

	return info, err
}

func getComponentInfo(name string, snapInfo *snap.Info) (*snap.ComponentInfo, error) {
	mountDir := snap.ComponentMountDir(name, snapInfo.InstanceName(), snapInfo.Revision)
	container := snapdir.New(mountDir)
	return snap.ReadComponentInfoFromContainer(container, snapInfo)
}

func createOrUpdateUserDataSymlink(info *snap.Info, usr *user.User, opts *dirs.SnapDirOptions) error {
	// 'current' symlink for user data (SNAP_USER_DATA)
	userData := info.UserDataDir(usr.HomeDir, opts)
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

func createUserDataDirs(info *snap.Info, opts *dirs.SnapDirOptions) error {
	if opts == nil {
		opts = &dirs.SnapDirOptions{}
	}

	// Adjust umask so that the created directories have the permissions we
	// expect and are unaffected by the initial umask. While go runtime creates
	// threads at will behind the scenes, the setting of umask applies to the
	// entire process so it doesn't need any special handling to lock the
	// executing goroutine to a single thread.
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	usr, err := userCurrent()
	if err != nil {
		return fmt.Errorf(i18n.G("cannot get the current user: %v"), err)
	}

	snapDir := snap.SnapDir(usr.HomeDir, opts)
	if err := os.MkdirAll(snapDir, 0700); err != nil {
		return fmt.Errorf(i18n.G("cannot create snap home dir: %w"), err)
	}
	// see snapenv.User
	instanceUserData := info.UserDataDir(usr.HomeDir, opts)
	instanceCommonUserData := info.UserCommonDataDir(usr.HomeDir, opts)
	createDirs := []string{instanceUserData, instanceCommonUserData}

	if info.InstanceKey != "" {
		// parallel instance snaps get additional mapping in their mount
		// namespace, namely /home/joe/snap/foo_bar ->
		// /home/joe/snap/foo, make sure that the mount point exists and
		// is owned by the user
		snapUserDir := snap.UserSnapDir(usr.HomeDir, info.SnapName(), opts)
		createDirs = append(createDirs, snapUserDir)
	}
	for _, d := range createDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			// TRANSLATORS: %q is the directory whose creation failed, %v the error message
			return fmt.Errorf(i18n.G("cannot create %q: %v"), d, err)
		}
	}

	if err := createOrUpdateUserDataSymlink(info, usr, opts); err != nil {
		return err
	}

	return maybeRestoreSecurityContext(usr, opts)
}

// maybeRestoreSecurityContext attempts to restore security context of ~/snap on
// systems where it's applicable
func maybeRestoreSecurityContext(usr *user.User, opts *dirs.SnapDirOptions) error {
	snapUserHome := snap.SnapDir(usr.HomeDir, opts)
	enabled, err := selinuxIsEnabled()
	if err != nil {
		return fmt.Errorf("cannot determine SELinux status: %v", err)
	}
	if !enabled {
		logger.Debugf("SELinux not enabled")
		return nil
	}

	match, err := selinuxVerifyPathContext(snapUserHome)
	if err != nil {
		return fmt.Errorf("failed to verify SELinux context of %v: %v", snapUserHome, err)
	}
	if match {
		return nil
	}
	logger.Noticef("restoring default SELinux context of %v", snapUserHome)

	if err := selinuxRestoreContext(snapUserHome, selinux.RestoreMode{Recursive: true}); err != nil {
		return fmt.Errorf("cannot restore SELinux context of %v: %v", snapUserHome, err)
	}
	return nil
}

func (x *cmdRun) useStrace() bool {
	// make sure the go-flag parser ran and assigned default values
	return x.ParserRan == 1 && x.Strace != "no-strace"
}

func (x *cmdRun) straceOpts() (opts []string, raw bool, err error) {
	if x.Strace == "with-strace" {
		return nil, false, nil
	}

	split, err := shlex.Split(x.Strace)
	if err != nil {
		return nil, false, err
	}

	opts = make([]string, 0, len(split))
	for _, opt := range split {
		switch {
		case opt == "--raw":
			raw = true
			continue

		case opt == "--output" || opt == "-o" ||
			strings.HasPrefix(opt, "--output=") ||
			strings.HasPrefix(opt, "-o="):
			// the user may have redirected strace output to a file,
			// in which case we cannot filter out
			// strace-confine/strace-exec call chain
			raw = true
		}

		opts = append(opts, opt)
	}
	return opts, raw, nil
}

func (x *cmdRun) snapRunApp(snapApp string, args []string) error {
	if x.DebugLog {
		os.Setenv("SNAPD_DEBUG", "1")
		logger.Debugf("enabled debug logging of early snap startup")
	}
	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := getSnapInfo(snapName, snap.R(0))
	if err != nil {
		return err
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf(i18n.G("cannot find app %q in %q"), appName, snapName)
	}

	if !app.IsService() {
		// TODO: use signal.NotifyContext as context when snap-run flow is finalized
		if err := maybeWaitWhileInhibited(context.Background(), snapName); err != nil {
			return err
		}
	}

	runner := newAppRunnable(info, app)

	return x.runSnapConfine(info, runner, args)
}

func (x *cmdRun) snapRunHook(snapTarget string) error {
	snapInstance, componentName := snap.SplitSnapComponentInstanceName(snapTarget)

	revision, err := snap.ParseRevision(x.Revision)
	if err != nil {
		return err
	}

	info, err := getSnapInfo(snapInstance, revision)
	if err != nil {
		return err
	}

	var (
		hook      *snap.HookInfo
		component *snap.ComponentInfo
	)
	if componentName == "" {
		hook = info.Hooks[x.HookName]
	} else {
		// TODO: we need to figure out how to get the component revision to set
		// the environment variables that we provide to the hook
		componentRevision := snap.Revision{}
		_ = componentRevision

		component, err = getComponentInfo(componentName, info)
		if err != nil {
			return err
		}
		hook = component.Hooks[x.HookName]
	}

	if hook == nil {
		return fmt.Errorf(i18n.G("cannot find hook %q in %q"), x.HookName, snapTarget)
	}

	// compoment may be nil here, meaning that this is a hook for the snap
	// itself, not a component hook
	runner := newHookRunnable(info, hook, component)

	return x.runSnapConfine(info, runner, nil)
}

func (x *cmdRun) snapRunTimer(snapApp, timer string, args []string) error {
	schedule, err := timeutil.ParseSchedule(timer)
	if err != nil {
		return fmt.Errorf("invalid timer format: %v", err)
	}

	now := timeNow()
	if !timeutil.Includes(schedule, now) {
		fmt.Fprintf(Stderr, "%s: attempted to run %q timer outside of scheduled time %q\n", now.Format(time.RFC3339), snapApp, timer)
		return nil
	}

	return x.snapRunApp(snapApp, args)
}

var osReadlink = os.Readlink

// snapdHelperPath return the path of a helper like "snap-confine" or
// "snap-exec" based on if snapd is re-execed or not
func snapdHelperPath(toolName string) (string, error) {
	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		return "", fmt.Errorf("cannot read /proc/self/exe: %v", err)
	}
	// no re-exec
	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		return filepath.Join(dirs.DistroLibExecDir, toolName), nil
	}
	// The logic below only works if the last two path components
	// are /usr/bin
	// FIXME: use a snap warning?
	if !strings.HasSuffix(exe, "/usr/bin/"+filepath.Base(exe)) {
		logger.Noticef("(internal error): unexpected exe input in snapdHelperPath: %v", exe)
		return filepath.Join(dirs.DistroLibExecDir, toolName), nil
	}
	// snapBase will be "/snap/{core,snapd}/$rev/" because
	// the snap binary is always at $root/usr/bin/snap
	snapBase := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	// Run snap-confine from the core/snapd snap.  The tools in
	// core/snapd snap are statically linked, or mostly
	// statically, with the exception of libraries such as libudev
	// and libc.
	return filepath.Join(snapBase, dirs.CoreLibExecDir, toolName), nil
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

func activateXdgDocumentPortal(runner runnable) error {
	// Don't do anything for apps or hooks that don't plug the
	// desktop interface
	//
	// NOTE: This check is imperfect because we don't really know
	// if the interface is connected or not but this is an
	// acceptable compromise for not having to communicate with
	// snapd in snap run. In a typical desktop session the
	// document portal can be in use by many applications, not
	// just by snaps, so this is at most, pre-emptively using some
	// extra memory.
	plugs := runner.Plugs()

	plugsDesktop := false
	for _, plug := range plugs {
		if plug.Interface == "desktop" {
			plugsDesktop = true
			break
		}
	}
	if !plugsDesktop {
		return nil
	}

	documentPortal := &portal.Document{}
	expectedMountPoint, err := documentPortal.GetDefaultMountPoint()
	if err != nil {
		return err
	}

	// If $XDG_RUNTIME_DIR/doc appears to be a mount point, assume
	// that the document portal is up and running.
	if mounted, err := osutil.IsMounted(expectedMountPoint); err != nil {
		logger.Noticef("Could not check document portal mount state: %s", err)
	} else if mounted {
		return nil
	}

	// If there is no session bus, our job is done.  We check this
	// manually to avoid dbus.SessionBus() auto-launching a new
	// bus.
	busAddress := osGetenv("DBUS_SESSION_BUS_ADDRESS")
	if len(busAddress) == 0 {
		return nil
	}

	// We've previously tried to start the document portal and
	// were told the service is unknown: don't bother connecting
	// to the session bus again.
	//
	// As the file is in $XDG_RUNTIME_DIR, it will be cleared over
	// full logout/login or reboot cycles.
	xdgRuntimeDir, err := documentPortal.GetUserXdgRuntimeDir()
	if err != nil {
		return err
	}

	portalsUnavailableFile := filepath.Join(xdgRuntimeDir, ".portals-unavailable")
	if osutil.FileExists(portalsUnavailableFile) {
		return nil
	}

	actualMountPoint, err := documentPortal.GetMountPoint()
	if err != nil {
		// It is not considered an error if
		// xdg-document-portal is not available on the system.
		if dbusErr, ok := err.(dbus.Error); ok && dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
			// We ignore errors here: if writing the file
			// fails, we'll just try connecting to D-Bus
			// again next time.
			if err = os.WriteFile(portalsUnavailableFile, []byte(""), 0644); err != nil {
				logger.Noticef("WARNING: cannot write file at %s: %s", portalsUnavailableFile, err)
			}
			return nil
		}
		return err
	}

	// Quick check to make sure the document portal is exposed
	// where we think it is.
	if actualMountPoint != expectedMountPoint {
		return fmt.Errorf(i18n.G("Expected portal at %#v, got %#v"), expectedMountPoint, actualMountPoint)
	}
	return nil
}

type envForExecFunc func(extra map[string]string) []string

var gdbServerWelcomeFmt = `
Welcome to "snap run --gdbserver".
You are right before your application is run.
Please open a different terminal and run:

gdb -ex="target remote %[1]s" -ex=continue -ex="signal SIGCONT"
(gdb) continue

or use your favorite gdb frontend and connect to %[1]s
`

func racyFindFreePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (x *cmdRun) useGdbserver() bool {
	// compatibility, can be removed after 2021
	if x.ExperimentalGdbserver != "no-gdbserver" {
		x.Gdbserver = x.ExperimentalGdbserver
	}

	// make sure the go-flag parser ran and assigned default values
	return x.ParserRan == 1 && x.Gdbserver != "no-gdbserver"
}

func (x *cmdRun) runCmdUnderGdbserver(origCmd []string, envForExec envForExecFunc) error {
	gcmd := exec.Command(origCmd[0], origCmd[1:]...)
	gcmd.Stdin = os.Stdin
	gcmd.Stdout = os.Stdout
	gcmd.Stderr = os.Stderr
	gcmd.Env = envForExec(map[string]string{"SNAP_CONFINE_RUN_UNDER_GDBSERVER": "1"})
	if err := gcmd.Start(); err != nil {
		return err
	}
	// wait for the child process executing gdb helper to raise SIGSTOP
	// signalling readiness to attach a gdbserver process
	var status syscall.WaitStatus
	_, err := syscall.Wait4(gcmd.Process.Pid, &status, syscall.WSTOPPED, nil)
	if err != nil {
		return err
	}

	addr := x.Gdbserver
	if addr == ":0" {
		// XXX: run "gdbserver :0" instead and parse "Listening on port 45971"
		//      on stderr instead?
		port, err := racyFindFreePort()
		if err != nil {
			return fmt.Errorf("cannot find free port: %v", err)
		}
		addr = fmt.Sprintf(":%v", port)
	}
	// XXX: should we provide a helper here instead? something like
	//      `snap run --attach-debugger` or similar? The downside
	//      is that attaching a gdb frontend is harder?
	fmt.Fprintf(Stdout, fmt.Sprintf(gdbServerWelcomeFmt, addr))
	// note that only gdbserver needs to run as root, the application
	// keeps running as the user
	gdbSrvCmd := exec.Command("sudo", "-E", "gdbserver", "--attach", addr, strconv.Itoa(gcmd.Process.Pid))
	if output, err := gdbSrvCmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func (x *cmdRun) runCmdUnderGdb(origCmd []string, envForExec envForExecFunc) error {
	// the resulting application process runs as root
	cmd := []string{"sudo", "-E", "gdb", "-ex=run", "-ex=catch exec", "-ex=continue", "--args"}
	cmd = append(cmd, origCmd...)

	gcmd := exec.Command(cmd[0], cmd[1:]...)
	gcmd.Stdin = os.Stdin
	gcmd.Stdout = os.Stdout
	gcmd.Stderr = os.Stderr
	gcmd.Env = envForExec(map[string]string{"SNAP_CONFINE_RUN_UNDER_GDB": "1"})
	return gcmd.Run()
}

func (x *cmdRun) runCmdWithTraceExec(origCmd []string, envForExec envForExecFunc) error {
	// setup private tmp dir with strace fifo
	straceTmp, err := ioutil.TempDir("", "exec-trace")
	if err != nil {
		return err
	}
	defer os.RemoveAll(straceTmp)
	straceLog := filepath.Join(straceTmp, "strace.fifo")
	if err := syscall.Mkfifo(straceLog, 0640); err != nil {
		return err
	}
	// ensure we have one writer on the fifo so that if strace fails
	// nothing blocks
	fw, err := os.OpenFile(straceLog, os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	defer fw.Close()

	// read strace data from fifo async
	var slg *strace.ExecveTiming
	var straceErr error
	doneCh := make(chan bool, 1)
	go func() {
		// FIXME: make this configurable?
		nSlowest := 10
		slg, straceErr = strace.TraceExecveTimings(straceLog, nSlowest)
		close(doneCh)
	}()

	cmd, err := strace.TraceExecCommand(straceLog, origCmd...)
	if err != nil {
		return err
	}
	// run
	cmd.Env = envForExec(nil)
	cmd.Stdin = Stdin
	cmd.Stdout = Stdout
	cmd.Stderr = Stderr
	err = cmd.Run()
	// ensure we close the fifo here so that the strace.TraceExecCommand()
	// helper gets a EOF from the fifo (i.e. all writers must be closed
	// for this)
	fw.Close()

	// wait for strace reader
	<-doneCh
	if straceErr == nil {
		slg.Display(Stderr)
	} else {
		logger.Noticef("cannot extract runtime data: %v", straceErr)
	}
	return err
}

func (x *cmdRun) runCmdUnderStrace(origCmd []string, envForExec envForExecFunc) error {
	extraStraceOpts, raw, err := x.straceOpts()
	if err != nil {
		return err
	}
	cmd, err := strace.Command(extraStraceOpts, origCmd...)
	if err != nil {
		return err
	}

	// run with filter
	cmd.Env = envForExec(nil)
	cmd.Stdin = Stdin
	if raw {
		// no output filtering, we can pass the child's stdout/stderr
		// directly
		cmd.Stdout = Stdout
		cmd.Stderr = Stderr

		return cmd.Run()
	}

	// note hijacking stdout, means it is no longer a tty and programs
	// expecting stdout to be on a terminal (eg. bash) may misbehave at this
	// point
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	filterDone := make(chan struct{})
	stdoutProxyDone := make(chan struct{})
	go func() {
		defer close(filterDone)

		r := bufio.NewReader(stderr)

		// The first thing from strace if things work is
		// "exeve(" - show everything until we see this to
		// not swallow real strace errors.
		for {
			s, err := r.ReadString('\n')
			if err != nil {
				break
			}
			if strings.Contains(s, "execve(") {
				break
			}
			fmt.Fprint(Stderr, s)
		}

		// The last thing that snap-exec does is to
		// execve() something inside the snap dir so
		// we know that from that point on the output
		// will be interessting to the user.
		//
		// We need check both /snap (which is where snaps
		// are located inside the mount namespace) and the
		// distro snap mount dir (which is different on e.g.
		// fedora/arch) to fully work with classic snaps.
		needle1 := fmt.Sprintf(`execve("%s`, dirs.SnapMountDir)
		needle2 := `execve("/snap`
		for {
			s, err := r.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(Stderr, "cannot read strace output: %s\n", err)
				}
				break
			}
			// Ensure we catch the execve but *not* the
			// exec into
			// /snap/core/current/usr/lib/snapd/snap-confine
			// which is just `snap run` using the core version
			// snap-confine.
			if (strings.Contains(s, needle1) || strings.Contains(s, needle2)) && !strings.Contains(s, "usr/lib/snapd/snap-confine") {
				fmt.Fprint(Stderr, s)
				break
			}
		}
		io.Copy(Stderr, r)
	}()

	go func() {
		defer close(stdoutProxyDone)
		io.Copy(Stdout, stdout)
	}()

	if err := cmd.Start(); err != nil {
		return err
	}
	<-filterDone
	<-stdoutProxyDone
	err = cmd.Wait()
	return err
}

func newHookRunnable(info *snap.Info, hook *snap.HookInfo, component *snap.ComponentInfo) runnable {
	return runnable{
		info:      info,
		component: component,
		hook:      hook,
	}
}

func newAppRunnable(info *snap.Info, app *snap.AppInfo) runnable {
	return runnable{
		info: info,
		app:  app,
	}
}

// runnable bundles together the potential things that we could be running. A
// few accessor methods are provided that delegate the request to the
// appropriate field, depending on what we are running.
type runnable struct {
	hook      *snap.HookInfo
	component *snap.ComponentInfo
	app       *snap.AppInfo
	info      *snap.Info
}

// SecurityTag returns the security tag for the thing being run. The tag could
// come from a snap hook, a component hook, or a snap app.
func (r *runnable) SecurityTag() string {
	if r.hook != nil {
		return r.hook.SecurityTag()
	}
	return r.app.SecurityTag()
}

// Target returns the string identifier of the thing that should be run. This
// could either be a component ref, a snap ref, or a snap ref with a specific
// app.
func (r *runnable) Target() string {
	if r.component != nil {
		return snap.SnapComponentName(r.info.InstanceName(), r.component.Component.ComponentName)
	}

	if r.hook != nil {
		return r.info.InstanceName()
	}

	return fmt.Sprintf("%s.%s", r.info.InstanceName(), r.app.Name)
}

// Plugs returns the plugs for the thing being run. The plugs could come from a
// snap hook, a component hook, or a snap app.
func (r *runnable) Plugs() map[string]*snap.PlugInfo {
	if r.hook != nil {
		return r.hook.Plugs
	}
	return r.app.Plugs
}

// Hook returns the hook that is going to be run, if there is one. Will be nil
// if running an app.
func (r *runnable) Hook() *snap.HookInfo {
	return r.hook
}

// Hook returns the hook that contains the thing to be run, if there is one.
// Currently, this will only be present when running a component hook.
func (r *runnable) Component() *snap.ComponentInfo {
	return r.component
}

// App returns the app that is going to be run, if there is one. Will be nil if
// running a hook or component hook.
func (r *runnable) App() *snap.AppInfo {
	return r.app
}

// Validate checks that the runnable is in a valid state. This is used to catch
// programmer errors.
func (r *runnable) Validate() error {
	if r.hook != nil && r.app != nil {
		return fmt.Errorf("internal error: hook and app cannot coexist in a runnable")
	}

	if r.component != nil && r.app != nil {
		return fmt.Errorf("internal error: component and app cannot coexist in a runnable")
	}

	return nil
}

func (x *cmdRun) runSnapConfine(info *snap.Info, runner runnable, args []string) error {
	// check for programmer error, should never happen
	if err := runner.Validate(); err != nil {
		return err
	}

	snapConfine, err := snapdHelperPath("snap-confine")
	if err != nil {
		return err
	}
	if !osutil.FileExists(snapConfine) {
		if runner.Hook() != nil {
			logger.Noticef("WARNING: skipping running hook %q of %q: missing snap-confine", runner.Hook().Name, runner.Target())
			return nil
		}
		return fmt.Errorf(i18n.G("missing snap-confine: try updating your core/snapd package"))
	}

	logger.Debugf("executing snap-confine from %s", snapConfine)

	opts, err := getSnapDirOptions(info.InstanceName())
	if err != nil {
		return fmt.Errorf("cannot get snap dir options: %w", err)
	}

	if err := createUserDataDirs(info, opts); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

	xauthPath, err := migrateXauthority(info)
	if err != nil {
		logger.Noticef("WARNING: cannot copy user Xauthority file: %s", err)
	}

	if err := activateXdgDocumentPortal(runner); err != nil {
		logger.Noticef("WARNING: cannot start document portal: %s", err)
	}

	cmd := []string{snapConfine}
	if info.NeedsClassic() {
		cmd = append(cmd, "--classic")
	}

	// this should never happen since we validate snaps with "base: none" and do not allow hooks/apps
	if info.Base == "none" {
		return fmt.Errorf(`cannot run hooks / applications with base "none"`)
	}
	if info.Base != "" {
		cmd = append(cmd, "--base", info.Base)
	} else {
		if info.Type() == snap.TypeKernel {
			// kernels have no explicit base, we use the boot base
			modelAssertion, err := x.client.CurrentModelAssertion()
			if err != nil {
				if runner.Hook() != nil {
					return fmt.Errorf("cannot get model assertion to setup kernel hook run: %v", err)
				} else {
					return fmt.Errorf("cannot get model assertion to setup kernel app run: %v", err)
				}
			}
			modelBase := modelAssertion.Base()
			if modelBase != "" {
				cmd = append(cmd, "--base", modelBase)
			}
		}
	}

	securityTag := runner.SecurityTag()
	cmd = append(cmd, securityTag)

	// when under confinement, snap-exec is run from 'core' snap rootfs
	snapExecPath := filepath.Join(dirs.CoreLibExecDir, "snap-exec")

	if info.NeedsClassic() {
		// running with classic confinement, carefully pick snap-exec we
		// are going to use
		snapExecPath, err = snapdHelperPath("snap-exec")
		if err != nil {
			return err
		}
	}
	cmd = append(cmd, snapExecPath)

	if x.Shell {
		cmd = append(cmd, "--command=shell")
	}
	if x.Gdb {
		cmd = append(cmd, "--command=gdb")
	}
	if x.useGdbserver() {
		cmd = append(cmd, "--command=gdbserver")
	}
	if x.Command != "" {
		cmd = append(cmd, "--command="+x.Command)
	}

	if runner.Hook() != nil {
		cmd = append(cmd, "--hook="+runner.Hook().Name)
	}

	// snap-exec is POSIXly-- options must come before positionals.
	cmd = append(cmd, runner.Target())
	cmd = append(cmd, args...)

	env, err := osutil.OSEnvironment()
	if err != nil {
		return err
	}

	snapenv.ExtendEnvForRun(env, info, runner.Component(), opts)

	if len(xauthPath) > 0 {
		// Environment is not nil here because it comes from
		// osutil.OSEnvironment and that guarantees this
		// property.
		env["XAUTHORITY"] = xauthPath
	}

	// on each run variant path this will be used once to get
	// the environment plus additions in the right form
	envForExec := func(extra map[string]string) []string {
		for varName, value := range extra {
			env[varName] = value
		}
		if !info.NeedsClassic() {
			return env.ForExec()
		}
		// For a classic snap, environment variables that are
		// usually stripped out by ld.so when starting a
		// setuid process are presevered by being renamed by
		// prepending PreservedUnsafePrefix -- which snap-exec
		// will remove, restoring the variables to their
		// original names.
		return env.ForExecEscapeUnsafe(snapenv.PreservedUnsafePrefix)
	}

	// Systemd automatically places services under a unique cgroup encoding the
	// security tag, but for apps and hooks we need to create a transient scope
	// with similar purpose ourselves.
	//
	// The way this happens is as follows:
	//
	// 1) Services are implemented using systemd service units. Starting a
	// unit automatically places it in a cgroup named after the service unit
	// name. Snapd controls the name of the service units thus indirectly
	// controls the cgroup name.
	//
	// 2) Non-services, including hooks, are started inside systemd
	// transient scopes. Scopes are a systemd unit type that are defined
	// programmatically and are meant for groups of processes started and
	// stopped by an _arbitrary process_ (ie, not systemd). Systemd
	// requires that each scope is given a unique name. We employ a scheme
	// where random UUID is combined with the name of the security tag
	// derived from snap application or hook name. Multiple concurrent
	// invocations of "snap run" will use distinct UUIDs.
	//
	// Transient scopes allow launched snaps to integrate into
	// the systemd design. See:
	// https://www.freedesktop.org/wiki/Software/systemd/ControlGroupInterface/
	//
	// Programs running as root, like system-wide services and programs invoked
	// using tools like sudo are placed under system.slice. Programs running as
	// a non-root user are placed under user.slice, specifically in a scope
	// specific to a logind session.
	//
	// This arrangement allows for proper accounting and control of resources
	// used by snap application processes of each type.
	//
	// For more information about systemd cgroups, including unit types, see:
	// https://www.freedesktop.org/wiki/Software/systemd/ControlGroupInterface/
	needsTracking := true

	if app := runner.App(); app != nil && app.IsService() {
		// If we are running a service app then we do not need to use
		// application tracking. Services, both in the system and user scope,
		// do not need tracking because systemd already places them in a
		// tracking cgroup, named after the systemd unit name, and those are
		// sufficient to identify both the snap name and the app name.
		needsTracking = false
		// however it is still possible that the app (which is a
		// service) was invoked by the user, so it may be running inside
		// a user's scope cgroup, in which case separate tracking group
		// needs to be established
		if err := cgroupConfirmSystemdServiceTracking(securityTag); err != nil {
			if err == cgroup.ErrCannotTrackProcess {
				// we are not being tracked in a service cgroup
				// after all, go ahead and create a transient
				// scope
				needsTracking = true
				logger.Debugf("service app not tracked by systemd")
			} else {
				return err
			}
		}
	}
	// Allow using the session bus for all apps but not for hooks.
	allowSessionBus := runner.Hook() == nil
	// Track, or confirm existing tracking from systemd.
	if needsTracking {
		opts := &cgroup.TrackingOptions{AllowSessionBus: allowSessionBus}
		if err = cgroupCreateTransientScopeForTracking(securityTag, opts); err != nil {
			if err != cgroup.ErrCannotTrackProcess {
				return err
			}
			// If we cannot track the process then log a debug message.
			// TODO: if we could, create a warning. Currently this is not possible
			// because only snapd can create warnings, internally.
			logger.Debugf("snapd cannot track the started application")
			logger.Debugf("snap refreshes will not be postponed by this process")
		}
	}
	logger.StartupStageTimestamp("snap to snap-confine")
	if x.TraceExec {
		return x.runCmdWithTraceExec(cmd, envForExec)
	} else if x.Gdb {
		return x.runCmdUnderGdb(cmd, envForExec)
	} else if x.useGdbserver() {
		if _, err := exec.LookPath("gdbserver"); err != nil {
			// TODO: use xerrors.Is(err, exec.ErrNotFound) once
			// we moved off from go-1.9
			if execErr, ok := err.(*exec.Error); ok {
				if execErr.Err == exec.ErrNotFound {
					return fmt.Errorf("please install gdbserver on your system")
				}
			}
			return err
		}
		return x.runCmdUnderGdbserver(cmd, envForExec)
	} else if x.useStrace() {
		return x.runCmdUnderStrace(cmd, envForExec)
	} else {
		return syscallExec(cmd[0], cmd, envForExec(nil))
	}
}

func getSnapDirOptions(snap string) (*dirs.SnapDirOptions, error) {
	var opts dirs.SnapDirOptions

	data, err := ioutil.ReadFile(filepath.Join(dirs.SnapSeqDir, snap+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return &opts, nil
	} else if err != nil {
		return nil, err
	}

	var seq struct {
		MigratedToHiddenDir   bool `json:"migrated-hidden"`
		MigratedToExposedHome bool `json:"migrated-exposed-home"`
	}
	if err := json.Unmarshal(data, &seq); err != nil {
		return nil, err
	}

	opts.HiddenSnapDataDir = seq.MigratedToHiddenDir
	opts.MigratedToExposedHome = seq.MigratedToExposedHome

	return &opts, nil
}

var cgroupCreateTransientScopeForTracking = cgroup.CreateTransientScopeForTracking
var cgroupConfirmSystemdServiceTracking = cgroup.ConfirmSystemdServiceTracking
