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
    int n=0, m;
    for (; *p != '\0';) {
        if ((m = skip_lowercase_letters(&p)) > 0) {
            n += m;
            got_letter = true;
            continue;
        }
        if ((m = skip_digits(&p)) > 0) {
            n += m;
            continue;
        }
        if (skip_one_char(&p, '-') > 0) {
            n++;
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
    if (n > 40) {
        bootstrap_msg = "snap name must be shorter than 40 characters";
        return -1;
    }

    bootstrap_msg = NULL;
    return 0;
}

// process_arguments parses given a command line
// argc and argv are defined as for the main() function
void process_arguments(int argc, char *const *argv, const char** snap_name_out, bool* should_setns_out)
{
    // Find the name of the called program. If it is ending with ".test" then do nothing.
    // NOTE: This lets us use cgo/go to write tests without running the bulk
    // of the code automatically.
    //
    if (argv == NULL || argc < 1) {
        bootstrap_errno = 0;
        bootstrap_msg = "argv0 is corrupted";
        return;
    }
    const char* argv0 = argv[0];
    const char* argv0_suffix_maybe = strstr(argv0, ".test");
    if (argv0_suffix_maybe != NULL && argv0_suffix_maybe[strlen(".test")] == '\0') {
        bootstrap_errno = 0;
        bootstrap_msg = "bootstrap is not enabled while testing";
        return;
    }

    bool should_setns = true;
    const char* snap_name = NULL;

    // Sanity check the command line arguments.  The go parts will
    // scan this too.
    int i;
    for (i = 1; i < argc; i++) {
        const char *arg = argv[i];
        if (arg[0] == '-') {
            /* We have an option */
            if (!strcmp(arg, "--from-snap-confine")) {
                // When we are running under "--from-snap-confine"
                // option skip the setns call as snap-confine has
                // already placed us in the right namespace.
                should_setns = false;
            } else {
                bootstrap_errno = 0;
                bootstrap_msg = "unsupported option";
                return;
            }
        } else {
            // We expect a single positional argument: the snap name
            if (snap_name != NULL) {
                bootstrap_errno = 0;
                bootstrap_msg = "too many positional arguments";
                return;
            }
            snap_name = arg;
        }
    }

    // If there's no snap name given, just bail out.
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
void bootstrap(int argc, char **argv, char **envp)
{
    // We may have been started via a setuid-root snap-confine. In order to
    // prevent environment-based attacks we start by erasing all environment
    // variables.
    char *snapd_debug = getenv("SNAPD_DEBUG");
    if (clearenv() != 0) {
        bootstrap_errno = 0;
        bootstrap_msg = "bootstrap could not clear the environment";
        return;
    }
    if (snapd_debug != NULL) {
        setenv("SNAPD_DEBUG", snapd_debug, 0);
    }

    // Analyze the read process cmdline to find the snap name and decide if we
    // should use setns to jump into the mount namespace of a particular snap.
    // This is spread out for easier testability.
    const char* snap_name = NULL;
    bool should_setns = false;
    process_arguments(argc, argv, &snap_name, &should_setns);
    if (snap_name != NULL && should_setns) {
        setns_into_snap(snap_name);
        // setns_into_snap sets bootstrap_{errno,msg}
    }
}
