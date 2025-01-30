/*
 * Copyright (C) 2016 Canonical Ltd
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

#include "system-shutdown-utils.h"

#include <errno.h>       // errno, sys_errlist
#include <fcntl.h>       // open
#include <linux/loop.h>  // LOOP_CLR_FD
#include <linux/major.h>
#include <stdarg.h>      // va_*
#include <stdio.h>       // fprintf, stderr
#include <stdlib.h>      // exit
#include <string.h>      // strcmp, strncmp
#include <sys/ioctl.h>   // ioctl
#include <sys/mount.h>   // umount
#include <sys/reboot.h>  // reboot, RB_*
#include <sys/stat.h>    // mkdir
#include <unistd.h>      // getpid, close

#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

__attribute__((format(printf, 1, 2))) void kmsg(const char *fmt, ...) {
    static FILE *kmsg = NULL;
    static char *head = NULL;
    if (!kmsg) {
        // TODO: figure out why writing to /dev/kmsg doesn't work from here
        kmsg = stderr;
        head = "snapd system-shutdown helper: ";
    }

    va_list va;
    va_start(va, fmt);
    fputs(head, kmsg);
    vfprintf(kmsg, fmt, va);
    fprintf(kmsg, "\n");
    va_end(va);
}

int sc_read_reboot_arg(char *arg, size_t max_size) {
    FILE *f;

    // This file is used by systemd to pass around a reboot parameter See
    // https://github.com/systemd/systemd/blob/v229/src/basic/def.h#L44
    f = fopen("/run/systemd/reboot-param", "r");
    if (!f) {
        return -1;
    }

    if (!fgets(arg, max_size, f)) {
        fclose(f);
        return -1;
    }
    arg[strcspn(arg, "\n")] = '\0';

    kmsg("reboot arg is %s", arg);
    fclose(f);
    return 0;
}

static void detach_loop(const char *src) {
    int fd = open(src, O_RDONLY);
    if (fd < 0) {
        kmsg("* unable to open loop device %s: %s", src, strerror(errno));
    } else {
        if (ioctl(fd, LOOP_CLR_FD) < 0) {
            kmsg("* unable to disassociate loop device %s: %s", src, strerror(errno));
        }
        close(fd);
    }
}

// tries to umount all (well, most) things. Returns whether in the last pass it
// no longer found writable.
bool umount_all(void) {
    bool did_umount = true;
    bool had_writable = false;

    for (int i = 0; i < 10 && did_umount; i++) {
        sc_mountinfo *mounts = sc_parse_mountinfo(NULL);
        if (!mounts) {
            // oh dear
            die("unable to get mount info; giving up");
        }
        sc_mountinfo_entry *cur = sc_first_mountinfo_entry(mounts);

        had_writable = false;
        did_umount = false;
        while (cur) {
            const char *dir = cur->mount_dir;
            const char *src = cur->mount_source;
            unsigned major = cur->dev_major;

            cur = sc_next_mountinfo_entry(cur);

            if (sc_streq("/", dir)) {
                continue;
            }

            if (sc_streq("/dev", dir)) {
                continue;
            }

            if (sc_streq("/proc", dir)) {
                continue;
            }

            if (major != 0 && major != LOOP_MAJOR && sc_endswith(dir, "/writable")) {
                had_writable = true;
            }

            if (umount(dir) == 0) {
                if (major == LOOP_MAJOR) {
                    detach_loop(src);
                }

                did_umount = true;
            }
        }
        sc_cleanup_mountinfo(&mounts);
    }

    return !had_writable;
}
