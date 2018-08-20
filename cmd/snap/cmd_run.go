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
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapenv"
	"github.com/snapcore/snapd/strutil/shlex"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/x11"
)

var (
	syscallExec = syscall.Exec
	userCurrent = user.Current
	osGetenv    = os.Getenv
	timeNow     = time.Now
)

type cmdRun struct {
	Command          string `long:"command" hidden:"yes"`
	HookName         string `long:"hook" hidden:"yes"`
	Revision         string `short:"r" default:"unset" hidden:"yes"`
	Shell            bool   `long:"shell" `
	SkipCommandChain bool   `long:"skip-command-chain"`

	// This options is both a selector (use or don't use strace) and it
	// can also carry extra options for strace. This is why there is
	// "default" and "optional-value" to distinguish this.
	Strace string `long:"strace" optional:"true" optional-value:"with-strace" default:"no-strace" default-mask:"-"`
	Gdb    bool   `long:"gdb"`

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
			"command":            i18n.G("Alternative command to run"),
			"hook":               i18n.G("Hook to run"),
			"r":                  i18n.G("Use a specific snap revision when running hook"),
			"shell":              i18n.G("Run a shell instead of the command (useful for debugging)"),
			"skip-command-chain": i18n.G("Do not run the command chain (useful for debugging)"),
			"strace":             i18n.G("Run the command under strace (useful for debugging). Extra strace options can be specified as well here. Pass --raw to strace early snap helpers."),
			"gdb":                i18n.G("Run the command with gdb"),
			"timer":              i18n.G("Run as a timer service with given schedule"),
			"parser-ran":         "",
		}, nil)
}

func maybeWaitForSecurityProfileRegeneration() error {
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

	cli := Client()
	for i := 0; i < timeout; i++ {
		if _, err := cli.SysInfo(); err == nil {
			return nil
		}
		// sleep a litte bit for good measure
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

	if err := maybeWaitForSecurityProfileRegeneration(); err != nil {
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
		snapUserDir := filepath.Join(usr.HomeDir, dirs.UserHomeSnapDir, info.SnapName())
		createDirs = append(createDirs, snapUserDir)
	}
	for _, d := range createDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			// TRANSLATORS: %q is the directory whose creation failed, %v the error message
			return fmt.Errorf(i18n.G("cannot create %q: %v"), d, err)
		}
	}

	return createOrUpdateUserDataSymlink(info, usr)
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

	// Try strace from the snap first, we use new syscalls like
	// "_newselect" that are known to not work with the strace of e.g.
	// ubuntu 14.04.
	//
	// TODO: some architectures do not have some syscalls (e.g.
	// s390x does not have _newselect). In
	// https://github.com/strace/strace/issues/57 options are
	// discussed.  We could use "-e trace=?syscall" but that is
	// only available since strace 4.17 which is not even in
	// ubutnu 17.10.
	var stracePath string
	cand := filepath.Join(dirs.SnapMountDir, "strace-static", "current", "bin", "strace")
	if osutil.FileExists(cand) {
		stracePath = cand
	}
	if stracePath == "" {
		stracePath, err = exec.LookPath("strace")
		if err != nil {
			return nil, fmt.Errorf("cannot find an installed strace, please try 'snap install strace-static'")
		}
	}

	return []string{
		sudoPath, "-E",
		stracePath,
		"-u", current.Username,
		"-f",
		// these syscalls are excluded because they make strace hang
		// on all or some architectures (gettimeofday on arm64)
		"-e", "!select,pselect6,_newselect,clock_gettime,sigaltstack,gettid,gettimeofday",
	}, nil
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

func (x *cmdRun) runCmdUnderStrace(origCmd, env []string) error {
	// prepend strace magic
	cmd, err := straceCmd()
	if err != nil {
		return err
	}
	straceOpts, raw, err := x.straceOpts()
	if err != nil {
		return err
	}
	cmd = append(cmd, straceOpts...)
	cmd = append(cmd, origCmd...)

	// run with filter
	gcmd := exec.Command(cmd[0], cmd[1:]...)
	gcmd.Env = env
	gcmd.Stdin = Stdin
	gcmd.Stdout = Stdout
	stderr, err := gcmd.StderrPipe()
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
	if err := gcmd.Start(); err != nil {
		return err
	}
	<-filterDone
	err = gcmd.Wait()
	return err
}

func (x *cmdRun) runSnapConfine(info *snap.Info, securityTag, snapApp, hook string, args []string) error {
	snapConfine := filepath.Join(dirs.DistroLibExecDir, "snap-confine")
	// if we re-exec, we must run the snap-confine from the core/snapd snap
	// as well, if they get out of sync, havoc will happen
	if isReexeced() {
		// exe is something like /snap/{snapd,core}/123/usr/bin/snap
		exe, err := osReadlink("/proc/self/exe")
		if err != nil {
			return err
		}
		// snapBase will be "/snap/{core,snapd}/$rev/" because
		// the snap binary is always at $root/usr/bin/snap
		snapBase := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
		// Run snap-confine from the core/snapd snap. That
		// will work because snap-confine on the core/snapd snap is
		// mostly statically linked (except libudev and libc)
		snapConfine = filepath.Join(snapBase, dirs.CoreLibExecDir, "snap-confine")
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

	cmd := []string{snapConfine}
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

	if x.Shell {
		cmd = append(cmd, "--command=shell")
	}
	if x.Gdb {
		cmd = append(cmd, "--command=gdb")
	}
	if x.Command != "" {
		cmd = append(cmd, "--command="+x.Command)
	}
	if x.SkipCommandChain {
		cmd = append(cmd, "--skip-command-chain")
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

	if x.Gdb {
		return x.runCmdUnderGdb(cmd, env)
	} else if x.useStrace() {
		return x.runCmdUnderStrace(cmd, env)
	} else {
		return syscallExec(cmd[0], cmd, env)
	}
}
