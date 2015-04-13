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
	"strconv"
	"strings"
	"syscall"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"
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

func readUid(user, passwdFile string) (uid int, err error) {
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

func unpackAndDropPrivs(snapFile, targetDir, rootDir string) error {

	d, err := clickdeb.Open(snapFile)
	if err != nil {
		return err
	}

	if helpers.ShouldDropPrivs() {

		passFile := passwdFile(rootDir, "passwd")
		uid, err := readUid(dropPrivsUser, passFile)
		if err != nil {
			return err
		}

		groupFile := passwdFile(rootDir, "group")
		gid, err := readUid(dropPrivsUser, groupFile)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}

		if err := os.Chown(targetDir, uid, gid); err != nil {
			return err
		}

		// run prctl(PR_SET_NO_NEW_PRIVS)
		rc := C.prctl_no_new_privs()
		if rc < 0 {
			return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS) failed with %v", rc)
		}

		if err := syscall.Setgroups([]int{gid}); err != nil {
			return err
		}

		if err := syscall.Setgid(gid); err != nil {
			return err
		}
		if err := syscall.Setuid(uid); err != nil {
			return err
		}

		// extra paranoia
		if syscall.Getuid() != uid || syscall.Getgid() != gid {
			return fmt.Errorf("Dropping privileges failed, uid is %v, gid is %v", syscall.Getuid(), syscall.Getgid())
		}
	}

	return d.Unpack(targetDir)
}

func init() {
	var cmdInternalUnpackData cmdInternalUnpack
	if _, err := parser.AddCommand("internal-unpack", "internal", "internal", &cmdInternalUnpackData); err != nil {
		// panic here as something must be terribly wrong if there is an
		// error here
		panic(err)
	}
}

func (x *cmdInternalUnpack) Execute(args []string) (err error) {
	return unpackAndDropPrivs(x.Positional.SnapFile, x.Positional.TargetDir, x.Positional.RootDir)
}
