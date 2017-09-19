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

// IMPORTANT: all the code in this file may be run with elevated privileges
// when invoking snap-update-ns from the setuid snap-confine.
//
// This file is a preprocessor for snap-update-ns' main() function. It will
// perform input validation and clear the environment so that snap-update-ns'
// go code runs with safe inputs when called by the setuid() snap-confine.

#include "bootstrap.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <sched.h>
#include <stdbool.h>
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
    // Ensure buf is initialized to all NULLs since our parsing of cmdline with
    // its embedded NULLs depends on this. Use for loop instead of memset() to
    // guarantee this initialization won't be optimized away.
    size_t i;
    for (i = 0; i < buf_size; ++i) {
        buf[i] = '\0';
    }
    ssize_t num_read = read(fd, buf, buf_size);
    if (num_read < 0) {
        bootstrap_errno = errno;
        bootstrap_msg = "cannot read /proc/self/cmdline";
    } else if (num_read == buf_size && buf_size > 0 && buf[buf_size - 1] != '\0') {
        bootstrap_errno = 0;
        bootstrap_msg = "cannot fit all of /proc/self/cmdline, buffer too small";
        num_read = -1;
    }
    close(fd);
    return num_read;
}

// find_snap_name scans the command line buffer and looks for the 1st argument.
// if the 1st argument exists but is empty NULL is returned.
const char*
find_snap_name(const char* buf, size_t num_read)
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

    const char* snap_name = buf;
    if (*snap_name == '\0') {
        return NULL;
    }
    return snap_name;
}

const char*
find_1st_option(const char* buf, size_t num_read)
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
    char buf[PATH_MAX] = {
        0,
    };
    int n = snprintf(buf, sizeof buf, "/run/snapd/ns/%s.mnt", snap_name);
    if (n >= sizeof buf || n < 0) {
        bootstrap_errno = 0;
        bootstrap_msg = "cannot format mount namespace file name";
        return -1;
    }

    // Open the mount namespace file.
    int fd = open(buf, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
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

// TODO: reuse the code from snap-confine, if possible.
static int skip_lowercase_letters(const char** p)
{
    int skipped = 0;
    const char* c;
    for (c = *p; *c >= 'a' && *c <= 'z'; ++c) {
        skipped += 1;
    }
    *p = (*p) + skipped;
    return skipped;
}

// TODO: reuse the code from snap-confine, if possible.
static int skip_digits(const char** p)
{
    int skipped = 0;
    const char* c;
    for (c = *p; *c >= '0' && *c <= '9'; ++c) {
        skipped += 1;
    }
    *p = (*p) + skipped;
    return skipped;
}

// TODO: reuse the code from snap-confine, if possible.
static int skip_one_char(const char** p, char c)
{
    if (**p == c) {
        *p += 1;
        return 1;
    }
    return 0;
}

// validate_snap_name performs full validation of the given name.
int validate_snap_name(const char* snap_name)
{
    // NOTE: This function should be synchronized with the two other
    // implementations: sc_snap_name_validate and snap.ValidateName.

    // Ensure that name is not NULL
    if (snap_name == NULL) {
        bootstrap_msg = "snap name cannot be NULL";
        return -1;
    }
    // This is a regexp-free routine hand-codes the following pattern:
    //
    // "^([a-z0-9]+-?)*[a-z](-?[a-z0-9])*$"
    //
    // The only motivation for not using regular expressions is so that we
    // don't run untrusted input against a potentially complex regular
    // expression engine.
    const char* p = snap_name;
    if (skip_one_char(&p, '-')) {
        bootstrap_msg = "snap name cannot start with a dash";
        return -1;
    }
    bool got_letter = false;
    for (; *p != '\0';) {
        if (skip_lowercase_letters(&p) > 0) {
            got_letter = true;
            continue;
        }
        if (skip_digits(&p) > 0) {
            continue;
        }
        if (skip_one_char(&p, '-') > 0) {
            if (*p == '\0') {
                bootstrap_msg = "snap name cannot end with a dash";
                return -1;
            }
            if (skip_one_char(&p, '-') > 0) {
                bootstrap_msg = "snap name cannot contain two consecutive dashes";
                return -1;
            }
            continue;
        }
        bootstrap_msg = "snap name must use lower case letters, digits or dashes";
        return -1;
    }
    if (!got_letter) {
        bootstrap_msg = "snap name must contain at least one letter";
        return -1;
    }

    bootstrap_msg = NULL;
    return 0;
}

// process_arguments parses given cmdline which must be list of strings separated with NUL bytes.
void process_arguments(const char* cmdline, size_t num_read, const char** snap_name_out, bool* should_setns_out)
{
    // Find the name of the called program. If it is ending with ".test" then do nothing.
    // NOTE: This lets us use cgo/go to write tests without running the bulk
    // of the code automatically.
    //
    // cmdline is an array of NUL ('\0') separated strings and guaranteed to be
    // NULL-terminated via read_cmdline().
    const char* argv0 = cmdline;
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

    // When we are running under "--from-snap-confine" option skip the setns
    // call as snap-confine has already placed us in the right namespace.
    const char* option = find_1st_option(cmdline, (size_t)num_read);
    bool should_setns = true;
    if (option != NULL) {
        if (strncmp(option, "--from-snap-confine", strlen("--from-snap-confine")) == 0) {
            should_setns = false;
        } else {
            bootstrap_errno = 0;
            bootstrap_msg = "unsupported option";
            return;
        }
    }

    // Find the name of the snap by scanning the cmdline.  If there's no snap
    // name given, just bail out. The go parts will scan this too.
    const char* snap_name = find_snap_name(cmdline, num_read);
    if (snap_name == NULL) {
        bootstrap_errno = 0;
        bootstrap_msg = "snap name not provided";
        return;
    }

    // Ensure that the snap name is valid so that we don't blindly setns into
    // something that is controlled by a potential attacker.
    if (validate_snap_name(snap_name) < 0) {
        bootstrap_errno = 0;
        // bootstap_msg is set by validate_snap_name;
        return;
    }
    // We have a valid snap name now so let's store it.
    if (snap_name_out != NULL) {
        *snap_name_out = snap_name;
    }
    if (should_setns_out != NULL) {
        *should_setns_out = should_setns;
    }
    bootstrap_errno = 0;
    bootstrap_msg = NULL;
}

// bootstrap prepares snap-update-ns to work in the namespace of the snap given
// on command line.
void bootstrap(void)
{
    // We may have been started via a setuid-root snap-confine. In order to
    // prevent environment-based attacks we start by erasing all environment
    // variables.
    if (clearenv() != 0) {
        bootstrap_errno = 0;
        bootstrap_msg = "bootstrap could not clear the environment";
        return;
    }
    // We don't have argc/argv so let's imitate that by reading cmdline
    // NOTE: use explicit initialization to avoid optimizing-out memset.
    char cmdline[1024] = {
        0,
    };
    ssize_t num_read;
    if ((num_read = read_cmdline(cmdline, sizeof cmdline)) < 0) {
        // read_cmdline sets bootstrap_{errno,msg}
        return;
    }

    // Analyze the read process cmdline to find the snap name and decide if we
    // should use setns to jump into the mount namespace of a particular snap.
    // This is spread out for easier testability.
    const char* snap_name = NULL;
    bool should_setns = false;
    process_arguments(cmdline, (size_t)num_read, &snap_name, &should_setns);
    if (snap_name != NULL && should_setns) {
        setns_into_snap(snap_name);
        // setns_into_snap sets bootstrap_{errno,msg}
    }
}
