/*
 * Copyright (C) 2019 Canonical Ltd
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
#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "seccomp-support-ext.h"

#include <errno.h>
#include <linux/seccomp.h>
#include <stdio.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <unistd.h>

#include "../libsnap-confine-private/utils.h"

#ifndef SECCOMP_FILTER_FLAG_LOG
#define SECCOMP_FILTER_FLAG_LOG 2
#endif

#ifndef seccomp
// prototype because we build with -Wstrict-prototypes
int seccomp(unsigned int operation, unsigned int flags, void *args);

int seccomp(unsigned int operation, unsigned int flags, void *args) {
    errno = 0;
    return syscall(__NR_seccomp, operation, flags, args);
}
#endif

void sc_apply_seccomp_filter(struct sock_fprog *prog) {
    int err;

    // Load filter into the kernel (by this point we have dropped to the
    // calling user but still retain CAP_SYS_ADMIN).
    //
    // Importantly we are intentionally *not* setting NO_NEW_PRIVS because it
    // interferes with exec transitions in AppArmor with certain snapd
    // interfaces. Not setting NO_NEW_PRIVS does mean that applications can
    // adjust their sandbox if they have CAP_SYS_ADMIN or, if running on < 4.8
    // kernels, break out of the seccomp via ptrace. Both CAP_SYS_ADMIN and
    // 'ptrace (trace)' are blocked by AppArmor with typical snapd interfaces.
    err = seccomp(SECCOMP_SET_MODE_FILTER, SECCOMP_FILTER_FLAG_LOG, prog);
    if (err != 0) {
        /* The profile may fail to load using the "modern" interface.
         * In such case use the older prctl-based interface instead. */
        switch (errno) {
            case ENOSYS:
                debug("kernel doesn't support the seccomp(2) syscall");
                break;
            case EINVAL:
                debug("kernel may not support the SECCOMP_FILTER_FLAG_LOG flag");
                break;
        }
        debug("falling back to prctl(2) syscall to load seccomp filter");
        err = prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, prog);
        if (err != 0) {
            die("cannot apply seccomp profile");
        }
    }
}
