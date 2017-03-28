// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

// Use a pre-main helper to switch the mount namespace. This is required as
// golang creates threads at will and setns(..., CLONE_NEWNS) fails if any
// threads apart from the main thread exist.

/*
#define _GNU_SOURCE

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

// bootstrap_errno contains a copy of errno if a system call fails.
int bootstrap_errno = 0;
// bootstrap_msg contains a static string if something fails.
const char *bootstrap_msg = NULL;


// read_cmdline reads /proc/self/cmdline into the specified buffer, returning number of bytes read.
static ssize_t read_cmdline(char *buf, size_t buf_size) {
	int fd = open("/proc/self/cmdline", O_RDONLY|O_CLOEXEC|O_NOFOLLOW);
	if (fd < 0) {
		bootstrap_errno = errno;
		bootstrap_msg = "cannot open /proc/self/cmdline";
		return -1;
	}
	memset(buf, 0, buf_size);
	ssize_t num_read;
	if ((num_read = read(fd, buf, buf_size)) < 0) {
		bootstrap_errno = errno;
		bootstrap_msg = "cannot read /proc/self/cmdline";
	}
	close(fd);
	return num_read;
}

// find_snap_name scans the command line buffer and looks for the 1st argument.
static const char *find_snap_name(char *buf, size_t buf_size, size_t num_read) {
	// cmdline is an array of NUL ('\0') separated strings. We can skip over
	// the first entry (program name) and look at the second entry, in our case
	// it should be the snap name.
	size_t argv0_len = strnlen(buf, buf_size);
	if (argv0_len == num_read) {
		return NULL;
	}
	return &buf[argv0_len + 1];
}

// setns_into_snap switches mount namespace into that of a given snap.
static int setns_into_snap(const char *snap_name) {
	// Construct the name of the .mnt file to open.
	char buf[PATH_MAX];
	int n = snprintf(buf, sizeof buf, "/run/snapd/ns/%s.mnt", snap_name);
	if (n >= sizeof buf || n < 0) {
		bootstrap_errno = errno;
		bootstrap_msg = "cannot format mount namespace file name";
		return -1;
	}

	// Open the mount namespace file, note that we don't specify O_NOFOLLOW as
	// that file is always a special, broken symbolic link.
	int fd = open(buf, O_RDONLY|O_CLOEXEC);
	if (fd < 0) {
		bootstrap_errno = errno;
		bootstrap_msg = "cannot open mount namespace file";
		return -1;
	}

	// Switch to the mount namespace of the given snap.
	int err = setns(fd, CLONE_NEWNS);
	if (err < 0) {
		bootstrap_errno = errno;
		bootstrap_msg = "cannot switch mount namespace";
	};

	close(fd);
	return err;
}

__attribute__((constructor)) static void enter_namespace(void) {
	// We don't have argc/argv so let's imitate that by reading cmdline
	char cmdline[1024];
	memset(cmdline, 0, sizeof cmdline);
	ssize_t num_read;
	if ((num_read = read_cmdline(cmdline, sizeof cmdline)) < 0) {
		return;
	}

	// Find the name of the snap by scanning the cmdline.  If there's no snap
	// name given, just bail out. The go parts will scan this too.
	const char *snap_name = find_snap_name(cmdline, sizeof cmdline, num_read);
	if (*snap_name == '\0') {
		return;
	}

	// Switch the mount namespace to that of the snap given on command line.
	if (setns_into_snap(snap_name) < 0) {
		return;
	}
}

// NOTE: do not add anything before the following `import "C"'
*/
import "C"

import (
	"fmt"
	"syscall"
)

// bootstrapError returns error (if any) encountered in pre-main C code.
func bootstrapError() error {
	if C.bootstrap_msg == nil {
		return nil
	}
	errno := syscall.Errno(C.bootstrap_errno)
	if errno != 0 {
		return fmt.Errorf("%s: %s", C.GoString(C.bootstrap_msg), errno)
	}
	return fmt.Errorf("%s", C.GoString(C.bootstrap_msg))
}
