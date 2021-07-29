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
#include "config.h"

#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <string.h>
#include <sys/stat.h>

#include "cgroup-support.h"
#include "cleanup-funcs.h"
#include "snap.h"
#include "string-utils.h"
#include "utils.h"

#include "device-cgroup-support.h"

typedef struct sc_cgroup_fds {
    int devices_allow_fd;
    int devices_deny_fd;
    int cgroup_procs_fd;
} sc_cgroup_fds;

static sc_cgroup_fds sc_cgroup_fds_new(void) {
    /* Note that -1 is the neutral value for a file descriptor.
     * This is relevant as a cleanup handler for sc_cgroup_fds,
     * closes all file descriptors that are not -1. */
    sc_cgroup_fds empty = {-1, -1, -1};
    return empty;
}

struct sc_device_cgroup {
    bool is_v2;
    char *security_tag;
    union {
        struct {
            sc_cgroup_fds fds;
        } v1;
        struct {
            int cgroup_fd;
            int devmap_fd;
            char *tag;
        } v2;
    };
};

__attribute__((format(printf, 2, 3))) static void sc_dprintf(int fd, const char *format, ...);

static int sc_udev_open_cgroup_v1(const char *security_tag, int flags, sc_cgroup_fds *fds);
static void sc_cleanup_cgroup_fds(sc_cgroup_fds *fds);

static int _sc_cgroup_v1_init(sc_device_cgroup *self, int flags) {
    self->v1.fds = sc_cgroup_fds_new();

    /* initialize to something sane */
    if (sc_udev_open_cgroup_v1(self->security_tag, flags, &self->v1.fds) < 0) {
        if (flags == SC_DEVICE_CGROUP_FROM_EXISTING) {
            return -1;
        }
        die("cannot prepare cgroup v1 device hierarchy");
    }
    /* Deny device access by default.
     *
     * Write 'a' to devices.deny to remove all existing devices that were added
     * in previous launcher invocations, then add the static and assigned
     * devices. This ensures that at application launch the cgroup only has
     * what is currently assigned. */
    sc_dprintf(self->v1.fds.devices_deny_fd, "a");
    return 0;
}

static void _sc_cgroup_v1_close(sc_device_cgroup *self) { sc_cleanup_cgroup_fds(&self->v1.fds); }

static void _sc_cgroup_v1_action(int fd, int kind, int major, int minor) {
    if ((uint32_t)minor != SC_DEVICE_MINOR_ANY) {
        sc_dprintf(fd, "%c %u:%u rwm\n", (kind == S_IFCHR) ? 'c' : 'b', major, minor);
    } else {
        /* use a mask to allow/deny all minor devices for that major */
        sc_dprintf(fd, "%c %u:* rwm\n", (kind == S_IFCHR) ? 'c' : 'b', major);
    }
}

static void _sc_cgroup_v1_allow(sc_device_cgroup *self, int kind, int major, int minor) {
    _sc_cgroup_v1_action(self->v1.fds.devices_allow_fd, kind, major, minor);
}

static void _sc_cgroup_v1_deny(sc_device_cgroup *self, int kind, int major, int minor) {
    _sc_cgroup_v1_action(self->v1.fds.devices_deny_fd, kind, major, minor);
}

static void _sc_cgroup_v1_attach_pid(sc_device_cgroup *self, pid_t pid) {
    sc_dprintf(self->v1.fds.cgroup_procs_fd, "%i\n", getpid());
}


static void sc_device_cgroup_close(sc_device_cgroup *self);

sc_device_cgroup *sc_device_cgroup_new(const char *security_tag, int flags) {
    sc_device_cgroup *self = calloc(1, sizeof(sc_device_cgroup));
    if (self == NULL) {
        die("cannot allocate device cgroup wrapper");
    }
    self->is_v2 = sc_cgroup_is_v2();
    self->security_tag = sc_strdup(security_tag);

    int ret = 0;
    if (!self->is_v2) {
        ret = _sc_cgroup_v1_init(self, flags);
	}

    if (ret < 0) {
        sc_device_cgroup_close(self);
        return NULL;
    }

    return self;
}

static void sc_device_cgroup_close(sc_device_cgroup *self) {
    if (!self->is_v2) {
        _sc_cgroup_v1_close(self);
    }
    sc_cleanup_string(&self->security_tag);
    free(self);
}

void sc_device_cgroup_cleanup(sc_device_cgroup **self) {
    if (*self == NULL) {
        return;
    }
    sc_device_cgroup_close(*self);
    *self = NULL;
}

int sc_device_cgroup_allow(sc_device_cgroup *self, int kind, int major, int minor) {
    if (kind != S_IFCHR && kind != S_IFBLK) {
        die("unsupported device kind 0x%04x", kind);
    }
    if (!self->is_v2) {
        _sc_cgroup_v1_allow(self, kind, major, minor);
    }
    return 0;
}

int sc_device_cgroup_deny(sc_device_cgroup *self, int kind, int major, int minor) {
    if (kind != S_IFCHR && kind != S_IFBLK) {
        die("unsupported device kind 0x%04x", kind);
    }
    if (!self->is_v2) {
        _sc_cgroup_v1_deny(self, kind, major, minor);
    }
    return 0;
}

int sc_device_cgroup_attach_pid(sc_device_cgroup *self, pid_t pid) {
    if (!self->is_v2) {
        _sc_cgroup_v1_attach_pid(self, pid);
    }
    return 0;
}

static void sc_dprintf(int fd, const char *format, ...) {
    va_list ap1;
    va_list ap2;
    int n_expected, n_actual;

    va_start(ap1, format);
    va_copy(ap2, ap1);
    n_expected = vsnprintf(NULL, 0, format, ap2);
    n_actual = vdprintf(fd, format, ap1);
    if (n_actual == -1 || n_expected != n_actual) {
        die("cannot write to fd %d", fd);
    }
    va_end(ap2);
    va_end(ap1);
}

static int sc_udev_open_cgroup_v1(const char *security_tag, int flags, sc_cgroup_fds *fds) {
    /* Open /sys/fs/cgroup */
    const char *cgroup_path = "/sys/fs/cgroup";
    int SC_CLEANUP(sc_cleanup_close) cgroup_fd = -1;
    cgroup_fd = open(cgroup_path, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (cgroup_fd < 0) {
        die("cannot open %s", cgroup_path);
    }

    /* Open devices relative to /sys/fs/cgroup */
    const char *devices_relpath = "devices";
    int SC_CLEANUP(sc_cleanup_close) devices_fd = -1;
    devices_fd = openat(cgroup_fd, devices_relpath, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (devices_fd < 0) {
        die("cannot open %s/%s", cgroup_path, devices_relpath);
    }

    /* Open snap.$SNAP_NAME.$APP_NAME relative to /sys/fs/cgroup/devices,
     * creating the directory if necessary. Note that we always chown the
     * resulting directory to root:root. */
    const char *security_tag_relpath = security_tag;
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    if (mkdirat(devices_fd, security_tag_relpath, 0755) < 0) {
        if (errno != EEXIST) {
            die("cannot create directory %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
        }
    }
    (void)sc_set_effective_identity(old);

    int SC_CLEANUP(sc_cleanup_close) security_tag_fd = -1;
    security_tag_fd = openat(devices_fd, security_tag_relpath, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (security_tag_fd < 0) {
        die("cannot open %s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath);
    }

    /* Open devices.allow relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    const char *devices_allow_relpath = "devices.allow";
    int SC_CLEANUP(sc_cleanup_close) devices_allow_fd = -1;
    devices_allow_fd = openat(security_tag_fd, devices_allow_relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
    if (devices_allow_fd < 0) {
        die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, devices_allow_relpath);
    }

    /* Open devices.deny relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    const char *devices_deny_relpath = "devices.deny";
    int SC_CLEANUP(sc_cleanup_close) devices_deny_fd = -1;
    devices_deny_fd = openat(security_tag_fd, devices_deny_relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
    if (devices_deny_fd < 0) {
        if (flags == SC_DEVICE_CGROUP_FROM_EXISTING && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, devices_deny_relpath);
    }

    /* Open cgroup.procs relative to /sys/fs/cgroup/devices/snap.$SNAP_NAME.$APP_NAME */
    const char *cgroup_procs_relpath = "cgroup.procs";
    int SC_CLEANUP(sc_cleanup_close) cgroup_procs_fd = -1;
    cgroup_procs_fd = openat(security_tag_fd, cgroup_procs_relpath, O_WRONLY | O_CLOEXEC | O_NOFOLLOW);
    if (cgroup_procs_fd < 0) {
        if (flags == SC_DEVICE_CGROUP_FROM_EXISTING && errno == ENOENT) {
            return -1;
        }
        die("cannot open %s/%s/%s/%s", cgroup_path, devices_relpath, security_tag_relpath, cgroup_procs_relpath);
    }

    /* Everything worked so pack the result and "move" the descriptors over so
     * that they are not closed by the cleanup functions associated with the
     * individual variables. */
    fds->devices_allow_fd = devices_allow_fd;
    fds->devices_deny_fd = devices_deny_fd;
    fds->cgroup_procs_fd = cgroup_procs_fd;
    /* Reset the locals so that they are not closed by the cleanup handlers. */
    devices_allow_fd = -1;
    devices_deny_fd = -1;
    cgroup_procs_fd = -1;
    return 0;
}

static void sc_cleanup_cgroup_fds(sc_cgroup_fds *fds) {
    if (fds != NULL) {
        sc_cleanup_close(&fds->devices_allow_fd);
        sc_cleanup_close(&fds->devices_deny_fd);
        sc_cleanup_close(&fds->cgroup_procs_fd);
    }
}
