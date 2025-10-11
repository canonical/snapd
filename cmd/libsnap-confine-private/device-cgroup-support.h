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

#ifndef SNAP_CONFINE_DEVICE_CGROUP_SUPPORT_H
#define SNAP_CONFINE_DEVICE_CGROUP_SUPPORT_H

#include <stdint.h>
#include <unistd.h>

struct sc_device_cgroup;
typedef struct sc_device_cgroup sc_device_cgroup;

enum {
    /* when creating a device cgroup wrapped, do not set up a new cgroup but
     * rather use an existing one */
    SC_DEVICE_CGROUP_FROM_EXISTING = 1,
};

/**
 * sc_device_cgroup_new returns a new cgroup device wrapper that is suitable for
 * the current system. Flags can contain SC_DEVICE_CGROUP_FROM_EXISTING in which
 * case an existing cgroup will be used, and a -1 return value with errno set to
 * ENOENT indicates that the group was not found. Otherwise, a new device cgroup
 * for a given tag will be set up.
 */
sc_device_cgroup *sc_device_cgroup_new(const char *security_tag, int flags);
/**
 * sc_device_cgroup_cleanup disposes of the cgroup wrapper and is suitable for
 * use with SC_CLEANUP
 */
void sc_device_cgroup_cleanup(sc_device_cgroup **self);

/**
 * SC_DEVICE_MINOR_ANY is used to indicate any minor device.
 */
static const uint32_t SC_DEVICE_MINOR_ANY = UINT32_MAX;

/**
 * sc_device_cgroup_allow sets up the cgroup to allow access to a given device
 * or a set of devices if SC_MINOR_ANY is passed as the minor number. The kind
 * must be one of S_IFCHR, S_IFBLK.
 */
int sc_device_cgroup_allow(sc_device_cgroup *self, int kind, int major, int minor);

/**
 * sc_device_cgroup_deny sets up the cgroup to deny access to a given device or
 * a set of devices if SC_MINOR_ANY is passed as the minor number. The kind must
 * be one of S_IFCHR, S_IFBLK.
 */
int sc_device_cgroup_deny(sc_device_cgroup *self, int kind, int major, int minor);

/**
 * sc_device_cgroup_attach_pid attaches given process ID to the associated
 * cgroup.
 */
int sc_device_cgroup_attach_pid(sc_device_cgroup *self, pid_t pid);

#endif /* SNAP_CONFINE_DEVICE_CGROUP_SUPPORT_H */
