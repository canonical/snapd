// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"fmt"
	"io"
	"io/ioutil"
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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/strace"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/snap"
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

	// This options is both a selector (use or don't use strace) and it
	// can also carry extra options for strace. This is why there is
	// "default" and "optional-value" to distinguish this.
	Strace    string `long:"strace" optional:"true" optional-value:"with-strace" default:"no-strace" default-mask:"-"`
	Gdb       bool   `long:"gdb"`
	TraceExec bool   `long:"trace-exec"`

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
			"gdb": i18n.G("Run the command with gdb"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"timer": i18n.G("Run as a timer service with given schedule"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"trace-exec": i18n.G("Display exec calls timing data"),
			"parser-ran": "",
		}, nil)
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

	for i := 0; i < timeout; i++ {
		if _, err := cli.SysInfo(); err == nil {
			return nil
		}
		// sleep a little bit for good measure
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for snap system profiles to get updated")
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

	// see snapenv.User
	instanceUserData := info.UserDataDir(usr.HomeDir)
	instanceCommonUserData := info.UserCommonDataDir(usr.HomeDir)
	createDirs := []string{instanceUserData, instanceCommonUserData}
	if info.InstanceKey != "" {
		// parallel instance snaps get additional mapping in their mount
		// namespace, namely /home/joe/snap/foo_bar ->
		// /home/joe/snap/foo, make sure that the mount point exists and
		// is owned by the user
		snapUserDir := snap.UserSnapDir(usr.HomeDir, info.SnapName())
		createDirs = append(createDirs, snapUserDir)
	}
	for _, d := range createDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			// TRANSLATORS: %q is the directory whose creation failed, %v the error message
			return fmt.Errorf(i18n.G("cannot create %q: %v"), d, err)
		}
	}

	if err := createOrUpdateUserDataSymlink(info, usr); err != nil {
		return err
	}

	return maybeRestoreSecurityContext(usr)
}

// maybeRestoreSecurityContext attempts to restore security context of ~/snap on
// systems where it's applicable
func maybeRestoreSecurityContext(usr *user.User) error {
	snapUserHome := filepath.Join(usr.HomeDir, dirs.UserHomeSnapDir)
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
		if opt == "--raw" {
			raw = true
			continue
		}
		opts = append(opts, opt)
	}
	return opts, raw, nil
}

func (x *cmdRun) snapRunApp(snapApp string, args []string) error {
	snapName, appName := snap.SplitSnapApp(snapApp)
	info, err := getSnapInfo(snapName, snap.R(0))
	if err != nil {
		return err
	}

	app := info.Apps[appName]
	if app == nil {
		return fmt.Errorf(i18n.G("cannot find app %q in %q"), appName, snapName)
	}

	return x.runSnapConfine(info, app.SecurityTag(), snapApp, "", args)
}

func (x *cmdRun) snapRunHook(snapName string) error {
	revision, err := snap.ParseRevision(x.Revision)
	if err != nil {
		return err
	}

	info, err := getSnapInfo(snapName, revision)
	if err != nil {
		return err
	}

	hook := info.Hooks[x.HookName]
	if hook == nil {
		return fmt.Errorf(i18n.G("cannot find hook %q in %q"), x.HookName, snapName)
	}

	return x.runSnapConfine(info, hook.SecurityTag(), snapName, hook.Name, nil)
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

func activateXdgDocumentPortal(info *snap.Info, snapApp, hook string) error {
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
	var plugs map[string]*snap.PlugInfo
	if hook != "" {
		plugs = info.Hooks[hook].Plugs
	} else {
		_, appName := snap.SplitSnapApp(snapApp)
		plugs = info.Apps[appName].Plugs
	}
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

	u, err := userCurrent()
	if err != nil {
		return fmt.Errorf(i18n.G("cannot get the current user: %s"), err)
	}
	xdgRuntimeDir := filepath.Join(dirs.XdgRuntimeDirBase, u.Uid)

	// If $XDG_RUNTIME_DIR/doc appears to be a mount point, assume
	// that the document portal is up and running.
	expectedMountPoint := filepath.Join(xdgRuntimeDir, "doc")
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
	portalsUnavailableFile := filepath.Join(xdgRuntimeDir, ".portals-unavailable")
	if osutil.FileExists(portalsUnavailableFile) {
		return nil
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}

	portal := conn.Object("org.freedesktop.portal.Documents",
		"/org/freedesktop/portal/documents")
	var mountPoint []byte
	if err := portal.Call("org.freedesktop.portal.Documents.GetMountPoint", 0).Store(&mountPoint); err != nil {
		// It is not considered an error if
		// xdg-document-portal is not available on the system.
		if dbusErr, ok := err.(dbus.Error); ok && dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
			// We ignore errors here: if writing the file
			// fails, we'll just try connecting to D-Bus
			// again next time.
			if err = ioutil.WriteFile(portalsUnavailableFile, []byte(""), 0644); err != nil {
				logger.Noticef("WARNING: cannot write file at %s: %s", portalsUnavailableFile, err)
			}
			return nil
		}
		return err
	}

	// Sanity check to make sure the document portal is exposed
	// where we think it is.
	actualMountPoint := strings.TrimRight(string(mountPoint), "\x00")
	if actualMountPoint != expectedMountPoint {
		return fmt.Errorf(i18n.G("Expected portal at %#v, got %#v"), expectedMountPoint, actualMountPoint)
	}
	return nil
}

func (x *cmdRun) runCmdUnderGdb(origCmd, env []string) error {
	env = append(env, "SNAP_CONFINE_RUN_UNDER_GDB=1")

	cmd := []string{"sudo", "-E", "gdb", "-ex=run", "-ex=catch exec", "-ex=continue", "--args"}
	cmd = append(cmd, origCmd...)

	gcmd := exec.Command(cmd[0], cmd[1:]...)
	gcmd.Stdin = os.Stdin
	gcmd.Stdout = os.Stdout
	gcmd.Stderr = os.Stderr
	gcmd.Env = env
	return gcmd.Run()
}

func (x *cmdRun) runCmdWithTraceExec(origCmd, env []string) error {
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
	cmd.Env = env
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

func (x *cmdRun) runCmdUnderStrace(origCmd, env []string) error {
	extraStraceOpts, raw, err := x.straceOpts()
	if err != nil {
		return err
	}
	cmd, err := strace.Command(extraStraceOpts, origCmd...)
	if err != nil {
		return err
	}

	// run with filter
	cmd.Env = env
	cmd.Stdin = Stdin
	cmd.Stdout = Stdout
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	filterDone := make(chan bool, 1)
	go func() {
		defer func() { filterDone <- true }()

		if raw {
			// Passing --strace='--raw' disables the filtering of
			// early strace output. This is useful when tracking
			// down issues with snap helpers such as snap-confine,
			// snap-exec ...
			io.Copy(Stderr, stderr)
			return
		}

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
	if err := cmd.Start(); err != nil {
		return err
	}
	<-filterDone
	err = cmd.Wait()
	return err
}

func (x *cmdRun) runSnapConfine(info *snap.Info, securityTag, snapApp, hook string, args []string) error {
	snapConfine, err := snapdHelperPath("snap-confine")
	if err != nil {
		return err
	}
	if !osutil.FileExists(snapConfine) {
		if hook != "" {
			logger.Noticef("WARNING: skipping running hook %q of snap %q: missing snap-confine", hook, info.InstanceName())
			return nil
		}
		return fmt.Errorf(i18n.G("missing snap-confine: try updating your core/snapd package"))
	}

	if err := createUserDataDirs(info); err != nil {
		logger.Noticef("WARNING: cannot create user data directory: %s", err)
	}

	xauthPath, err := migrateXauthority(info)
	if err != nil {
		logger.Noticef("WARNING: cannot copy user Xauthority file: %s", err)
	}

	if err := activateXdgDocumentPortal(info, snapApp, hook); err != nil {
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
	}
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
	if x.Command != "" {
		cmd = append(cmd, "--command="+x.Command)
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

	// Systemd automatically places services under a unique cgroup encoding the
	// security tag, for apps and hooks we need to create a transient scope
	// with similar purpose.
	//
	// The way this happens is as follows:
	//
	// 1) Services are implemented using systemd service units. Starting an
	// unit automatically places it in a cgroup named after the service unit
	// name. Snapd controls the name of the service units thus, indirectly, of
	// the cgroup name.
	//
	// 2) Non-services are started inside systemd transient scopes. Scopes are
	// a systemd unit type that is defined programmatically and is meant for
	// groups of processes started and stopped by _arbitrary process_, that is,
	// not systemd, through fork. This model fits snap applications very well.
	//
	// Services are placed under system.slice (for system services) or
	// user.slice (for user services). Non-service applications and hooks are
	// placed in a hierarchy representing the invoking user, typically inside a
	// particular session.
	//
	// This arrangement allows for proper accounting and control of resources
	// used by snap application processes of each type.
	//
	// For more information about systemd cgroups, including unit types, see:
	// https://www.freedesktop.org/wiki/Software/systemd/ControlGroupInterface/
	if app := info.Apps[snapApp]; app == nil || !app.IsService() {
		logger.Debugf("creating transient scope %s", securityTag)
		if err := createTransientScope(securityTag); err != nil {
			return err
		}
	} else {
		// TODO verify that the service is placed in a .service path in one of
		// the applicable cgroups.
	}
	if x.TraceExec {
		return x.runCmdWithTraceExec(cmd, env)
	} else if x.Gdb {
		return x.runCmdUnderGdb(cmd, env)
	} else if x.useStrace() {
		return x.runCmdUnderStrace(cmd, env)
	} else {
		return syscallExec(cmd[0], cmd, env)
	}
}

func randomUUID() (string, error) {
	// The source of the bytes generated here is the same as that of
	// /dev/urandom. In essence, this doens't block but is not crypto-strong.
	// Our goal here is to avoid clashing UUIDs that are needed for all of the
	// non-service commands that are started with the help of this UUID.
	uuidBytes, err := ioutil.ReadFile("/proc/sys/kernel/random/uuid")
	return strings.TrimSpace(string(uuidBytes)), err
}

var createTransientScope = func(securityTag string) error {
	if !features.RefreshAppAwareness.IsEnabled() {
		return nil
	}

	// The scope is created with a DBus call to systemd running either on
	// system or session bus, depending on if we are starting a program as root
	// or as a regular user.
	var conn *dbus.Conn
	var err error
	if os.Getuid() == 0 {
		conn, err = dbus.SystemBus()
	} else {
		conn, err = dbus.SessionBus()
	}
	if err != nil {
		return err
	}

	// The property and auxUnit types are not well documented but can be traced
	// from systemd source code. Systemd defines the signature of
	// StartTransientUnit as "ssa(sv)a(sa(sv))". The signature can be
	// decomposed as follows:
	//
	// Partial documentation, at the time of this writing, is available at
	// https://www.freedesktop.org/wiki/Software/systemd/dbus/
	//
	// unitName string // name of the unit to start
	// jobMode string  // corresponds to --job-mode= (see systemctl(1) manual page)
	// properties []struct{
	//   Name string
	//   Value interface{}
	// } // properties describe properties of the started unit
	// auxUnits []struct {
	//   Name string
	//   Properties []struct{
	//   	Name string
	//   	Value interface{}
	//	 }
	// } // auxUnits describe any additional units to define.
	type property struct {
		Name  string
		Value interface{}
	}
	type auxUnit struct {
		Name  string
		Props []property
	}

	// We ask the kernel for a random UUID. We need one because each transient
	// scope needs a unique name. The unique name is comprosed of said UUID and
	// the snap security tag.
	uuid, err := randomUUID()
	if err != nil {
		return err
	}
	// Instead of enforcing uniqueness we could join an existing scope but this has some limitations:
	// - the originally started scope must be marked as a delegate, with all consequences.
	// - the method AttachProcessesToUnit is unavailable on Ubuntu 16.04
	unitName := fmt.Sprintf("snap.%s.%s.scope", uuid, strings.TrimPrefix(securityTag, "snap."))
	mode := "fail"
	properties := []property{{"PIDs", []uint{uint(os.Getpid())}}}
	aux := []auxUnit(nil)
	systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	call := systemd.Call(
		"org.freedesktop.systemd1.Manager.StartTransientUnit",
		0, /* call flags */
		unitName,
		mode,
		properties,
		aux,
	)
	var job dbus.ObjectPath
	if err := call.Store(&job); err != nil {
		if dbusErr, ok := err.(dbus.Error); ok && dbusErr.Name == "org.freedesktop.DBus.Error.UnknownMethod" {
			// The DBus API is not supported on this system. This can happen on
			// very old versions of Systemd, for instance on Ubuntu 14.04.
			return nil
		}
		return fmt.Errorf("cannot create transient scope: %s", err)
	}
	return nil
}
