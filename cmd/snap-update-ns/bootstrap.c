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

#include "bootstrap.h"

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
const char* bootstrap_msg = NULL;

// read_cmdline reads /proc/self/cmdline into the specified buffer, returning
// number of bytes read.
ssize_t read_cmdline(char* buf, size_t buf_size)
{
    int fd = open("/proc/self/cmdline", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
    if (fd < 0) {
        bootstrap_errno = errno;
        bootstrap_msg = "cannot open /proc/self/cmdline";
        return -1;
    }
    memset(buf, 0, buf_size);
    ssize_t num_read = read(fd, buf, buf_size);
    if (num_read < 0) {
        bootstrap_errno = errno;
        bootstrap_msg = "cannot read /proc/self/cmdline";
    } else if (num_read == buf_size) {
        bootstrap_errno = 0;
        bootstrap_msg = "cannot fit all of /proc/self/cmdline, buffer too small";
        num_read = -1;
    }
    close(fd);
    return num_read;
}

// find_argv0 scans the command line buffer and looks for the 0st argument.
const char*
find_argv0(char* buf, size_t num_read)
{
    // cmdline is an array of NUL ('\0') separated strings.
    size_t argv0_len = strnlen(buf, num_read);
    if (argv0_len == num_read) {
        // ensure that the buffer is properly terminated.
        return NULL;
    }
    return buf;
}

// find_snap_name scans the command line buffer and looks for the 1st argument.
// if the 1st argument exists but is empty NULL is returned.
const char*
find_snap_name(char* buf, size_t num_read)
{
    // cmdline is an array of NUL ('\0') separated strings.
    //
    // We can skip over the first entry (program name) and look at the second
    // entry, in our case it should be the snap name. We also want to skip any
    // arguments starting with "-" as those are command line options we are not
    // interested in them.

    // Skip the zeroth argument as well as any options.
    do {
        size_t arg_len = strnlen(buf, num_read);
        if (arg_len + 1 >= num_read) {
            return NULL;
        }
        num_read -= arg_len + 1;
        buf += arg_len + 1;
    } while (buf[0] == '-');

    char* snap_name = buf;
    if (*snap_name == '\0') {
        return NULL;
    }
    return snap_name;
}

const char*
find_1st_option(char* buf, size_t num_read)
{
    size_t argv0_len = strnlen(buf, num_read);
    if (argv0_len + 1 >= num_read) {
        return NULL;
    }
    size_t pos = argv0_len + 1;
    if (buf[pos] == '-') {
        return &buf[pos];
    }
    return NULL;
}

// setns_into_snap switches mount namespace into that of a given snap.
static int
setns_into_snap(const char* snap_name)
{
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
    int fd = open(buf, O_RDONLY | O_CLOEXEC);
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

// partially_validate_snap_name performs partial validation of the given name.
// The goal is to ensure that there are no / or .. in the name.
int partially_validate_snap_name(const char* snap_name)
{
    // NOTE: neither set bootstrap_{msg,errno} but the return value means that
    // bootstrap does nothing. The name is re-validated by golang.
    if (strstr(snap_name, "..") != NULL) {
        return -1;
    }
    if (strchr(snap_name, '/') != NULL) {
        return -1;
    }
}

static void neuter_environment()
{
    // We may have been started via a setuid-root snap-confine. In order to
    // prevent environment-based attacks we start by erasing all environment
    // variables.
    clearenv();
}

// bootstrap prepares snap-update-ns to work in the namespace of the snap given
// on command line.
void bootstrap(void)
{
    neuter_environment();
    // We don't have argc/argv so let's imitate that by reading cmdline
    char cmdline[1024];
    memset(cmdline, 0, sizeof cmdline);
    ssize_t num_read;
    if ((num_read = read_cmdline(cmdline, sizeof cmdline)) < 0) {
        return;
    }

    // Find the name of the called program. If it is ending with "-test" then do nothing.
    // NOTE: This lets us use cgo/go to write tests without running the bulk
    // of the code automatically. In snapd we can just set the required
    // environment variable.
    const char* argv0 = find_argv0(cmdline, (size_t)num_read);
    if (argv0 == NULL) {
        bootstrap_errno = 0;
        bootstrap_msg = "argv0 is corrupted";
        return;
    }
    const char* argv0_suffix_maybe = strstr(argv0, ".test");
    if (argv0_suffix_maybe != NULL && argv0_suffix_maybe[strlen(".test")] == '\0') {
        bootstrap_errno = 0;
        bootstrap_msg = "bootstrap is not enabled while testing";
        return;
    }

    // Find the name of the snap by scanning the cmdline.  If there's no snap
    // name given, just bail out. The go parts will scan this too.
    const char* snap_name = find_snap_name(cmdline, (size_t)num_read);
    if (snap_name == NULL) {
        return;
    }

    // Look for known offenders in the snap name (.. and /) and do nothing if
    // those are found. The golang code will validate snap name and print a
    // proper error message but this just ensures we don't try to open / setns
    // anything unusual.
    if (partially_validate_snap_name(snap_name) < 0) {
        return;
    }

    // When we are running under "--from-snap-confine" option skip the setns
    // call as snap-confine has already placed us in the right namespace.
    const char* option = find_1st_option(cmdline, (size_t)num_read);
    if (option != NULL && strncmp(option, "--from-snap-confine", strlen("--from-snap-confine")) == 0) {
        return;
    }

    // Switch the mount namespace to that of the snap given on command line.
    if (setns_into_snap(snap_name) < 0) {
        return;
    }
}
