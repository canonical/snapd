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
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/logger"
)

// #include <sys/prctl.h>
// #include <errno.h>
// int prctl_no_new_privs()
// {
//   // see prctl(2), needs linux3.5 at runtime
//   // use magic constant for PR_SET_NO_NEW_PRIVS to avoid it at buildtime
//   // (buildds are on linux3.2)
//   int ret = prctl(38, 1, 0, 0, 0);
//   if (ret < 0 && errno != EINVAL)
//      return ret;
//   return 0;
// }
import "C"

// for compat with the old snappy, once that is gone we can drop to a
// different user
const dropPrivsUser = "clickpkg"

type cmdInternalUnpack struct {
	Positional struct {
		SnapFile  string `positional-arg-name:"snap file" description:"INTERNAL ONLY"`
		TargetDir string `positional-arg-name:"target dir" description:"INTERNAL ONLY"`
		RootDir   string `positional-arg-name:"root dir" description:"INTERNAL ONLY"`
	} `positional-args:"yes"`
}

func passwdFile(rootDir, file string) string {
	inRootPasswdFile := filepath.Join(rootDir, "etc", file)
	if _, err := os.Stat(inRootPasswdFile); err == nil {
		return inRootPasswdFile
	}

	return filepath.Join("/etc/", file)
}

func readUID(user, passwdFile string) (uid int, err error) {
	f, err := os.Open(passwdFile)
	if err != nil {
		return -1, err
	}

	scannerf := bufio.NewScanner(f)
	for scannerf.Scan() {
		if err := scannerf.Err(); err != nil {
			return -1, err
		}

		line := scannerf.Text()
		splitLine := strings.Split(line, ":")
		if len(splitLine) > 2 && splitLine[0] == user {
			return strconv.Atoi(splitLine[2])
		}
	}

	return -1, errors.New("failed to find user uid/gid")
}

// copied from go 1.3 (almost) verbatim. The go authors removed this
// implementation from 1.4 because it doesn't apply to all threads,
// which confuses people. Note the use of LockOSThread below. Note
// also they didn't remove Setgroups *yet*, but probably will do so at
// some point. Further note that it's also possible that they change
// things around more; read issue 1435 (currently at
// https://github.com/golang/go/issues/1435) for more details.
func setgid(gid int) (err error) {
	_, _, e1 := syscall.RawSyscall(syscall.SYS_SETGID, uintptr(gid), 0, 0)
	if e1 != 0 {
		err = e1
	}
	return
}
func setuid(uid int) (err error) {
	_, _, e1 := syscall.RawSyscall(syscall.SYS_SETUID, uintptr(uid), 0, 0)
	if e1 != 0 {
		err = e1
	}
	return
}

func unpackAndDropPrivs(snapFile, targetDir, rootDir string) error {

	d, err := clickdeb.Open(snapFile)
	if err != nil {
		return err
	}
	defer d.Close()

	if helpers.ShouldDropPrivs() {

		passFile := passwdFile(rootDir, "passwd")
		uid, err := readUID(dropPrivsUser, passFile)
		if err != nil {
			return err
		}

		groupFile := passwdFile(rootDir, "group")
		gid, err := readUID(dropPrivsUser, groupFile)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}

		if err := os.Chown(targetDir, uid, gid); err != nil {
			return err
		}

		// Setuid and Setgid only apply to the current Linux thread, so make
		// sure we don't get moved.
		runtime.LockOSThread()

		// run prctl(PR_SET_NO_NEW_PRIVS)
		rc := C.prctl_no_new_privs()
		if rc < 0 {
			return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS) failed with %v", rc)
		}

		if err := syscall.Setgroups([]int{gid}); err != nil {
			return fmt.Errorf("Setgroups([]{%d}) call failed: %v", gid, err)
		}

		if err := setgid(gid); err != nil {
			return fmt.Errorf("Setgid(%d) call failed: %v", gid, err)
		}
		if err := setuid(uid); err != nil {
			return fmt.Errorf("Setuid(%d) call failed: %v", uid, err)
		}

		// extra paranoia
		if syscall.Getuid() != uid || syscall.Getgid() != gid {
			return fmt.Errorf("Dropping privileges failed, uid is %v, gid is %v", syscall.Getuid(), syscall.Getgid())
		}
	}

	return d.Unpack(targetDir)
}

func init() {
	_, err := parser.AddCommand("internal-unpack",
		"internal",
		"internal",
		&cmdInternalUnpack{})
	if err != nil {
		logger.Panicf("Unable to internal_unpack: %v", err)
	}
}

func (x *cmdInternalUnpack) Execute(args []string) (err error) {
	return unpackAndDropPrivs(x.Positional.SnapFile, x.Positional.TargetDir, x.Positional.RootDir)
}
