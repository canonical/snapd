package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"launchpad.net/snappy/clickdeb"
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
	} `positional-args:"yes"`
}

func unpackAndDropPrivs(snapFile, targetDir string) error {

	if syscall.Getuid() == 0 {
		u, err := user.Lookup(dropPrivsUser)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}

		var uid, gid int
		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
		gid, err = strconv.Atoi(u.Gid)
		if err != nil {
			return err
		}
		for _, p := range []string{snapFile, targetDir} {
			if err := os.Chown(p, uid, gid); err != nil {
				return err
			}
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
			return fmt.Errorf("Droping privileges failed, uid is %v, gid is %v", syscall.Getuid(), syscall.Getgid())
		}
	}

	d := clickdeb.ClickDeb{Path: snapFile}

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
	return unpackAndDropPrivs(x.Positional.SnapFile, x.Positional.TargetDir)
}
