// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

// privhelper is a little helper used to write/read/remove files.
//
// Go can't (before 1.10) drop privileges, so when running as root you
// need to exec something to read/write/remove a file as a user. You
// can't rely on just being root, as the file might be on a filesystem
// where root is squashed (notably NFS).
//
// For read/remove we could just call cat and rm, but for write we
// need something custom as we want the write to be atomic. So this
// helper is needed for the latter case at least; the other two are
// done here as well for consistency's sake. Furhtermore using su has
// issues as it pokes at the terminal, and nested sudos are also an
// issue, so we do the set[ug]id here directly.
//
// Usage: privhelper <uid> <gid> (read|write|remove) <filename>
//
//   In 'read' mode, the file is copied to standard output.
//   In 'write' mode, the file is copied from standard input.
//
// The exit status is significant:
//   0: success
//   1: bad arguments (usage printed to stderr)
//   2: got a os.IsNotExist error when attempting the operation
//   3: got some other error (details printed to stderr)
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
)

func read(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "open", err
	}
	defer f.Close()

	if _, err := io.Copy(os.Stdout, f); err != nil {
		return "copy", err
	}

	return "", nil
}

func write(filename string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return "mkdir", err
	}

	f, err := osutil.NewAtomicFile(filename, 0600, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return "create", err
	}
	defer f.Cancel() // if committed, cancel is a nop

	if _, err := io.Copy(f, os.Stdin); err != nil {
		return "copy", err
	}

	if err := f.Commit(); err != nil {
		return "commit", err
	}

	return "", nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: privhelper <uid> <gid> (read|write|remove) <filename>")
	fmt.Fprintln(os.Stderr, "exit with:  0: OK, 1: bad args, 2: does not exist, 3: other error (read stderr)")
	fmt.Fprintln(os.Stderr, "read/write use stdout/stdin")
	os.Exit(1)
}

func run(uid sys.UserID, gid sys.GroupID, verb, filename string) (string, error) {
	if err := sys.Setgid(gid); err != nil {
		return "setgid", err
	}

	if err := sys.Setuid(uid); err != nil {
		return "setuid", err
	}

	switch verb {
	case "remove":
		return "remove", os.Remove(filename)
	case "read":
		return read(filename)
	case "write":
		return write(filename)
	default:
		usage()
	}
	panic("can't happen")
}

func main() {
	// Setuid and Setgid are per-thread, and we don't do anything
	// with goroutines, so this is a'ight.
	runtime.LockOSThread()

	if len(os.Args) != 5 {
		usage()
	}

	uid, err := strconv.ParseUint(os.Args[1], 10, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uid should be a 32-bit unsigned int: %v", err)
		usage()
	}

	gid, err := strconv.ParseUint(os.Args[2], 10, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gid should be a 32-bit unsigned int: %v", err)
		usage()
	}

	if step, err := run(sys.UserID(uid), sys.GroupID(gid), os.Args[3], os.Args[4]); err != nil {
		if os.IsNotExist(err) {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "%s: %v", step, err)
		os.Exit(3)
	}
}
