/*
 * Copyright (C) 2021 Canonical Ltd
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
#include <errno.h>
#include <fnmatch.h>
#include <libgen.h>
#include <limits.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/device-cgroup-support.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

#include "snap-device-helper.h"

static unsigned long must_strtoul(char *str) {
    char *end = str;
    unsigned long val = strtoul(str, &end, 10);
    if (*end != '\0') {
        die("malformed number \"%s\"", str);
    }
    return val;
}

/* udev_to_security_tag converts a udev tag (snap_foo_bar) to security tag
 * (snap.foo.bar) */
static char *udev_to_security_tag(const char *udev_tag) {
    if (!sc_startswith(udev_tag, "snap_")) {
        die("malformed tag \"%s\"", udev_tag);
    }
    char *tag = sc_strdup(udev_tag);
    /* possible udev tags are:
     * snap_foo_bar
     * snap_foo_instance_bar
     * snap_foo_hook_hookname
     * snap_foo_instance_hook_hookname
     * convert those to:
     * snap.foo.bar
     * snap.foo_instance.bar
     * snap.foo.hook.hookname
     * snap.foo_instance.hook.hookname
     */
    size_t tag_len = strlen(tag);
    if (tag_len < strlen("snap_a_b") || tag_len > SNAP_SECURITY_TAG_MAX_LEN) {
        die("tag \"%s\" length %zu is incorrect", udev_tag, tag_len);
    }
    const size_t snap_prefix_len = strlen("snap_");
    /* we know that the tag at least has a snap_ prefix because it was checked
     * before */
    tag[snap_prefix_len - 1] = '.';
    char *snap_name_start = tag + snap_prefix_len;
    char *snap_name_end = NULL;

    /* find the last separator */
    char *last_sep = strrchr(tag, '_');
    if (last_sep == NULL) {
        die("missing app name in tag \"%s\"", udev_tag);
    }
    *last_sep = '.';
    /* we are left with the following possibilities:
     * snap.foo.bar
     * snap.foo_instance.bar
     * snap.foo_instance_hook.hookname
     * snap.foo_hook.hookname
     */
    char *more_sep = strchr(tag, '_');
    if (more_sep == NULL) {
        /* no more separators, we have snap.foo.bar */
        snap_name_end = last_sep;
    } else {
        /* we are left with the following possibilities:
         * snap.foo_instance.bar
         * snap.foo_instance_hook.hookname
         * snap.foo_hook.hookname
         */

        /* do we have another separator? */
        char *another_sep = strchr(more_sep + 1, '_');
        if (another_sep == NULL) {
            /* no, so we are left with the following possibilities:
             * snap.foo_instance.bar
             * snap.foo_hook.hookname
             *
             * there is ambiguity and we cannot correctly handle an instance named
             * 'hook' as snap.foo_hook.bar could be snap.foo.hook.bar or
             * snap.foo_hook.bar, for simplicity assume snap.foo.hook.bar more likely.
             */
            if (sc_startswith(more_sep, "_hook.")) {
                /* snap.foo_hook.bar -> snap.foo.hook.bar */
                *more_sep = '.';
                snap_name_end = more_sep;
            } else {
                snap_name_end = last_sep;
            }
        } else {
            /* we have found 2 separators, so this is the only possibility:
             * snap.foo_instance_hook.hookname
             * which should be converted to:
             * snap.foo_instance.hook.hookname
             */
            *another_sep = '.';
            snap_name_end = another_sep;
        }
    }
    if (snap_name_end <= snap_name_start) {
        die("missing snap name in tag \"%s\"", udev_tag);
    }

    /* let's validate the tag, but we need to extract the snap name first */
    char snap_instance[SNAP_INSTANCE_LEN + 1] = {0};
    size_t snap_instance_len = (size_t)(snap_name_end - snap_name_start);
    if (snap_instance_len >= sizeof(snap_instance)) {
        die("snap instance of tag \"%s\" is too long", udev_tag);
    }
    memcpy(snap_instance, snap_name_start, snap_instance_len);
    debug("snap instance \"%s\"", snap_instance);

    if (!sc_security_tag_validate(tag, snap_instance)) {
        die("security tag \"%s\" for snap \"%s\" is not valid", tag, snap_instance);
    }

    return tag;
}

/* sysroot can be mocked in tests */
const char *sysroot = "/";

int snap_device_helper_run(const struct sdh_invocation *inv) {
    const char *action = inv->action;
    const char *udev_tagname = inv->tagname;
    const char *devpath = inv->devpath;
    const char *majmin = inv->majmin;

    bool allow = false;

    if (strlen(majmin) < 3) {
        die("no or malformed major/minor \"%s\"", majmin);
    }
    if (strlen(devpath) <= strlen("/devices/")) {
        die("no or malformed devpath \"%s\"", devpath);
    }
    if (sc_streq(action, "add") || sc_streq(action, "change")) {
        allow = true;
    } else if (sc_streq(action, "remove")) {
        allow = false;
    } else {
        die("ERROR: unknown action \"%s\"", action);
    }

    char *security_tag SC_CLEANUP(sc_cleanup_string) = udev_to_security_tag(udev_tagname);

    int devtype = S_IFCHR;
    /* find out the actual subsystem */
    char sysdevsubsystem[PATH_MAX] = {0};
    char fullsubsystem[PATH_MAX] = {0};
    sc_must_snprintf(sysdevsubsystem, sizeof(sysdevsubsystem), "%s/sys/%s/subsystem", sysroot, devpath);
    if (readlink(sysdevsubsystem, fullsubsystem, sizeof(fullsubsystem)) < 0) {
        if (errno == ENOENT && sc_streq(action, "remove")) {
            // on removal the devices are going away, so it is possible that the
            // symlink is already gone, in which case try guessing the type like
            // the old shell-based snap-device-helper did:
            //
            // > char devices are .../nvme/nvme* but block devices are
            // > .../nvme/nvme*/nvme*n* and .../nvme/nvme*/nvme*n*p* so if have a
            // > device that has nvme/nvme*/nvme*n* in it, treat it as a block
            // > device
            if ((fnmatch("*/block/*", devpath, 0) == 0) || (fnmatch("*/nvme/nvme*/nvme*n*", devpath, 0) == 0)) {
                devtype = S_IFBLK;
            }
        } else {
            die("cannot read symlink %s", sysdevsubsystem);
        }
    } else {
        char *subsystem = basename(fullsubsystem);
        if (sc_streq(subsystem, "block")) {
            devtype = S_IFBLK;
        }
    }
    sc_device_cgroup *cgroup = sc_device_cgroup_new(security_tag, SC_DEVICE_CGROUP_FROM_EXISTING);
    if (!cgroup) {
        if (errno == ENOENT) {
            debug("device cgroup does not exist");
            return 0;
        }
        die("cannot create device cgroup wrapper");
    }

    /* the format is <major>:<minor> */
    char *major SC_CLEANUP(sc_cleanup_string) = sc_strdup(majmin);
    char *sep = strchr(major, ':');
    // sep is always \0 terminated so this checks if the part after ":" is empty
    if (sep == NULL || sep[1] == '\0') {
        /* not found, or a last character */
        die("malformed major:minor string: %s", major);
    }
    /* set an end for the major number string */
    *sep = '\0';
    sep++;
    char *minor = sep;

    int devmajor = must_strtoul(major);
    int devminor = must_strtoul(minor);
    debug("%s device type is %s, %d:%d", inv->action, (devtype == S_IFCHR) ? "char" : "block", devmajor, devminor);
    if (allow) {
        sc_device_cgroup_allow(cgroup, devtype, devmajor, devminor);
    } else {
        sc_device_cgroup_deny(cgroup, devtype, devmajor, devminor);
    }

    return 0;
}
