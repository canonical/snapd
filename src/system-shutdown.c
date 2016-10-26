#include <stdbool.h>            // bools
#include <stdarg.h>             // va_*
#include <sys/mount.h>          // umount
#include <sys/stat.h>           // mkdir
#include <unistd.h>             // getpid, close
#include <string.h>             // strcmp, strncmp
#include <stdlib.h>             // exit
#include <stdio.h>              // fprintf, stderr
#include <sys/ioctl.h>          // ioctl
#include <linux/loop.h>         // LOOP_CLR_FD
#include <sys/reboot.h>         // reboot, RB_*
#include <fcntl.h>              // open
#include <errno.h>              // errno, sys_errlist

#include "mountinfo.h"


__attribute__ ((format(printf, 1, 2)))
static void kmsg(const char *fmt, ...) {
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

__attribute__ ((noreturn))
static void die(const char *msg)
{
        if (errno == 0) {
                kmsg("*** %s", msg);
        } else {
                kmsg("*** %s: %s", msg, sys_errlist[errno]);
        }
        sync();
        reboot(RB_HALT_SYSTEM);
        exit(1);
}


// tries to umount all (well, most) things. Returns whether in the last pass it
// no longer found writable.
static bool umount_all() {
        bool did_umount = true;
        bool had_writable = false;

        for (int i=0; i<10 && did_umount; i++) {
                struct mountinfo *mounts = parse_mountinfo(NULL);
                if (!mounts) {
                        // oh dear
                        die("unable to get mount info; giving up");
                }
                struct mountinfo_entry *cur = first_mountinfo_entry(mounts);

                had_writable = false;
                did_umount = false;
                while (cur) {
                        const char* mount_dir = mountinfo_entry_mount_dir(cur);
                        const char* mount_src = mountinfo_entry_mount_source(cur);
                        cur = next_mountinfo_entry(cur);

                        if (strcmp("/", mount_dir) == 0) {
                                continue;
                        }

                        if (strcmp("/dev", mount_dir) == 0) {
                                continue;
                        }

                        if (strcmp("/proc", mount_dir) == 0) {
                                continue;
                        }

                        if (strstr(mount_dir, "writable")) {
                                had_writable = true;
                        }

                        if (umount(mount_dir) == 0) {
                                int fd = open(mount_src, O_RDONLY);
                                if (fd >=0) {
                                        ioctl(fd, LOOP_CLR_FD);
                                        close(fd);
                                }

                                did_umount = true;
                        }
                }
                cleanup_mountinfo(&mounts);
        }

        return !had_writable;
}


int main(int argc, char *argv[]) {
        errno = 0;
        if (getpid() != 1) {
                fprintf(stderr, "This is a shutdown helper program; don't call it directly.\n");
                exit(1);
        }

        kmsg("started.");

        if (mkdir("/writable", 0755) < 0) {
                die("unable to mkdir");
        }

        if (umount_all()) {
                kmsg("- found no hard-to-unmount writable partition.");
        } else {
                if (mount("/oldroot/writable", "/writable", NULL, MS_MOVE, NULL) < 0) {
                        die("cannot move writable out of the way");
                }

                bool ok = umount_all();
                kmsg("%c was %s to unmount writable cleanly", ok ? '-' : '*', ok? "able" : "*NOT* able");
                sync(); // shouldn't be needed, but just in case
        }

        // argv[1] can be one of at least: halt, reboot, poweroff.
        // FIXME: might also be kexec, hibernate or hybrid-sleep -- support those!

        int cmd = RB_HALT_SYSTEM;

        if (argc < 2) {
                kmsg("* called without verb; halting.");
        } else {
                if (strcmp("reboot", argv[1]) == 0) {
                        cmd = RB_AUTOBOOT;
                        kmsg("- rebooting.");
                } else if (strcmp("poweroff", argv[1]) == 0) {
                        cmd = RB_POWER_OFF;
                        kmsg("- powering off.");
                } else if (strcmp("halt", argv[1]) == 0) {
                        kmsg("- halting.");
                } else {
                        kmsg("* called with unsupported verb %s; halting.", argv[1]);
                }
        }

        reboot(cmd);

        return 0;
}
