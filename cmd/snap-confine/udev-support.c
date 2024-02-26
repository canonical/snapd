/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

#include <ctype.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <sys/types.h>
#include <unistd.h>

#include <libudev.h>

#include "../libsnap-confine-private/cgroup-support.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/device-cgroup-support.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "udev-support.h"

/* Allow access to common devices. */
static void sc_udev_allow_common(sc_device_cgroup *cgroup) {
    /* The devices we add here have static number allocation.
     * https://www.kernel.org/doc/html/v4.11/admin-guide/devices.html */
    sc_device_cgroup_allow(cgroup, S_IFCHR, 1, 3);  // /dev/null
    sc_device_cgroup_allow(cgroup, S_IFCHR, 1, 5);  // /dev/zero
    sc_device_cgroup_allow(cgroup, S_IFCHR, 1, 7);  // /dev/full
    sc_device_cgroup_allow(cgroup, S_IFCHR, 1, 8);  // /dev/random
    sc_device_cgroup_allow(cgroup, S_IFCHR, 1, 9);  // /dev/urandom
    sc_device_cgroup_allow(cgroup, S_IFCHR, 5, 0);  // /dev/tty
    sc_device_cgroup_allow(cgroup, S_IFCHR, 5, 1);  // /dev/console
    sc_device_cgroup_allow(cgroup, S_IFCHR, 5, 2);  // /dev/ptmx
}

/** Allow access to current and future PTY slaves.
 *
 * We unconditionally add them since we use a devpts newinstance. Unix98 PTY
 * slaves major are 136-143.
 *
 * See also:
 * https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
 **/
static void sc_udev_allow_pty_slaves(sc_device_cgroup *cgroup) {
    for (unsigned pty_major = 136; pty_major <= 143; pty_major++) {
        sc_device_cgroup_allow(cgroup, S_IFCHR, pty_major, SC_DEVICE_MINOR_ANY);
    }
}

/** Allow access to Nvidia devices.
 *
 * Nvidia modules are proprietary and therefore aren't in sysfs and can't be
 * udev tagged. For now, just add existing nvidia devices to the cgroup
 * unconditionally (AppArmor will still mediate the access).  We'll want to
 * rethink this if snapd needs to mediate access to other proprietary devices.
 *
 * Device major and minor numbers are described in (though nvidia-uvm currently
 * isn't listed):
 *
 * https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
 **/
static void sc_udev_allow_nvidia(sc_device_cgroup *cgroup) {
    struct stat sbuf;

    /* Allow access to /dev/nvidia0 through /dev/nvidia254 */
    for (unsigned nv_minor = 0; nv_minor < 255; nv_minor++) {
        char nv_path[15] = {0};  // /dev/nvidiaXXX
        sc_must_snprintf(nv_path, sizeof(nv_path), "/dev/nvidia%u", nv_minor);

        /* Stop trying to find devices after one is not found. In this manner,
         * we'll add /dev/nvidia0 and /dev/nvidia1 but stop trying to find
         * nvidia3 - nvidia254 if nvidia2 is not found. */
        if (stat(nv_path, &sbuf) < 0) {
            break;
        }
        sc_device_cgroup_allow(cgroup, S_IFCHR, major(sbuf.st_rdev), minor(sbuf.st_rdev));
    }

    if (stat("/dev/nvidiactl", &sbuf) == 0) {
        sc_device_cgroup_allow(cgroup, S_IFCHR, major(sbuf.st_rdev), minor(sbuf.st_rdev));
    }
    if (stat("/dev/nvidia-uvm", &sbuf) == 0) {
        sc_device_cgroup_allow(cgroup, S_IFCHR, major(sbuf.st_rdev), minor(sbuf.st_rdev));
    }
    if (stat("/dev/nvidia-modeset", &sbuf) == 0) {
        sc_device_cgroup_allow(cgroup, S_IFCHR, major(sbuf.st_rdev), minor(sbuf.st_rdev));
    }
}

/**
 * Allow access to /dev/uhid.
 *
 * Currently /dev/uhid isn't represented in sysfs, so add it to the device
 * cgroup if it exists and let AppArmor handle the mediation.
 **/
static void sc_udev_allow_uhid(sc_device_cgroup *cgroup) {
    struct stat sbuf;

    if (stat("/dev/uhid", &sbuf) == 0) {
        sc_device_cgroup_allow(cgroup, S_IFCHR, major(sbuf.st_rdev), minor(sbuf.st_rdev));
    }
}

/**
 * Allow access to /dev/net/tun
 *
 * When CONFIG_TUN=m, /dev/net/tun will exist but using it doesn't
 * autoload the tun module but also /dev/net/tun isn't udev tagged
 * until it is loaded. To work around this, if /dev/net/tun exists, add
 * it unconditionally to the cgroup and rely on AppArmor to mediate the
 * access. LP: #1859084
 **/
static void sc_udev_allow_dev_net_tun(sc_device_cgroup *cgroup) {
    struct stat sbuf;

    if (stat("/dev/net/tun", &sbuf) == 0) {
        sc_device_cgroup_allow(cgroup, S_IFCHR, major(sbuf.st_rdev), minor(sbuf.st_rdev));
    }
}

/**
 * Allow access to assigned devices.
 *
 * The snapd udev security backend uses udev rules to tag matching devices with
 * tags corresponding to snap applications. Here we interrogate udev and allow
 * access to all assigned devices.
 **/
static void sc_udev_allow_assigned_device(sc_device_cgroup *cgroup, struct udev_device *device) {
    const char *path = udev_device_get_syspath(device);
    dev_t devnum = udev_device_get_devnum(device);
    unsigned int major = major(devnum);
    unsigned int minor = minor(devnum);
    /* The manual page of udev_device_get_devnum says:
     * > On success, udev_device_get_devnum() returns the device type of
     * > the passed device. On failure, a device type with minor and major
     * > number set to 0 is returned. */
    if (major == 0 && minor == 0) {
        debug("cannot get major/minor numbers for syspath %s", path);
        return;
    }
    /* devnode is bound to the lifetime of the device and we cannot release
     * it separately. */
    const char *devnode = udev_device_get_devnode(device);
    if (devnode == NULL) {
        debug("cannot find /dev node from udev device");
        return;
    }
    debug("inspecting type of device: %s", devnode);
    struct stat file_info;
    if (stat(devnode, &file_info) < 0) {
        debug("cannot stat %s", devnode);
        return;
    }
    int devtype = file_info.st_mode & S_IFMT;
    if (devtype == S_IFBLK || devtype == S_IFCHR) {
        sc_device_cgroup_allow(cgroup, devtype, major, minor);
    }
}

static void sc_udev_setup_acls_common(sc_device_cgroup *cgroup) {
    /* Allow access to various devices. */
    sc_udev_allow_common(cgroup);
    sc_udev_allow_pty_slaves(cgroup);
    sc_udev_allow_nvidia(cgroup);
    sc_udev_allow_uhid(cgroup);
    sc_udev_allow_dev_net_tun(cgroup);
}

static char *sc_security_to_udev_tag(const char *security_tag) {
    char *udev_tag = sc_strdup(security_tag);
    for (char *c = strchr(udev_tag, '.'); c != NULL; c = strchr(c, '.')) {
        *c = '_';
    }
    return udev_tag;
}

static void sc_cleanup_udev(struct udev **udev) {
    if (udev != NULL && *udev != NULL) {
        udev_unref(*udev);
        *udev = NULL;
    }
}

static void sc_cleanup_udev_enumerate(struct udev_enumerate **enumerate) {
    if (enumerate != NULL && *enumerate != NULL) {
        udev_enumerate_unref(*enumerate);
        *enumerate = NULL;
    }
}

/* __sc_udev_device_has_current_tag will be filled at runtime if the libudev has
 * this symbol.
 *
 * Note that we could try to define udev_device_has_current_tag with a weak
 * attribute, which should in the normal case be the filled by ld.so when
 * loading snap-confined. However this was observed to work in practice only
 * when the binary itself is build with recent enough toolchain (eg. gcc &
 * binutils on Ubuntu 20.04)
 */
static int (*__sc_udev_device_has_current_tag)(struct udev_device *udev_device, const char *tag) = NULL;
static void setup_current_tags_support(void) {
    void *lib = dlopen("libudev.so.1", RTLD_NOW);
    if (lib == NULL) {
        debug("cannot load libudev.so.1: %s", dlerror());
        /* bit unexpected as we use the library from the host and it's stable */
        return;
    }
    /* check whether we have the symbol introduced in systemd v247 to inspect
     * the CURRENT_TAGS property */
    void *sym = dlsym(lib, "udev_device_has_current_tag");
    if (sym == NULL) {
        debug("cannot find current tags symbol: %s", dlerror());
        /* symbol is not found in the library version */
        (void)dlclose(lib);
        return;
    }
    debug("libudev has current tags support");
    __sc_udev_device_has_current_tag = sym;
    /* lib goes out of scope and is leaked but we need sym and hence
     * lib to be valid for the entire lifetime of the application
     * lifecycle so this is fine. */
    /* coverity[leaked_storage] */
}

void sc_setup_device_cgroup(const char *security_tag) {
    debug("setting up device cgroup");

    setup_current_tags_support();
    if (__sc_udev_device_has_current_tag == NULL) {
        debug("no current tags support present");
    }

    /* Derive the udev tag from the snap security tag.
     *
     * Because udev does not allow for dots in tag names, those are replaced by
     * underscores in snapd. We just match that behavior. */
    char *udev_tag SC_CLEANUP(sc_cleanup_string) = NULL;
    udev_tag = sc_security_to_udev_tag(security_tag);

    /* Use udev APIs to talk to udev-the-daemon to determine the list of
     * "devices" with that tag assigned. The list may be empty, in which case
     * there's no udev tagging in effect and we must refrain from constructing
     * the cgroup as it would interfere with the execution of a program. */
    struct udev SC_CLEANUP(sc_cleanup_udev) *udev = NULL;
    udev = udev_new();
    if (udev == NULL) {
        die("cannot connect to udev");
    }
    struct udev_enumerate SC_CLEANUP(sc_cleanup_udev_enumerate) *devices = NULL;
    devices = udev_enumerate_new(udev);
    if (devices == NULL) {
        die("cannot create udev device enumeration");
    }
    if (udev_enumerate_add_match_tag(devices, udev_tag) < 0) {
        die("cannot add tag match to udev device enumeration");
    }
    if (udev_enumerate_scan_devices(devices) < 0) {
        die("cannot enumerate udev devices");
    }
    /* NOTE: udev_list_entry is bound to life-cycle of the used udev_enumerate
     */
    struct udev_list_entry *assigned;
    assigned = udev_enumerate_get_list_entry(devices);
    if (assigned == NULL) {
        /* NOTE: Nothing is assigned, don't create or use the device cgroup. */
        debug("no devices tagged with %s, skipping device cgroup setup", udev_tag);
        return;
    }

    /* cgroup wrapper is lazily initialized when devices are actually
     * assigned */
    sc_device_cgroup *cgroup SC_CLEANUP(sc_device_cgroup_cleanup) = NULL;
    for (struct udev_list_entry *entry = assigned; entry != NULL; entry = udev_list_entry_get_next(entry)) {
        const char *path = udev_list_entry_get_name(entry);
        if (path == NULL) {
            die("udev_list_entry_get_name failed");
        }
        struct udev_device *device = udev_device_new_from_syspath(udev, path);
        /** This is a non-fatal error as devices can disappear asynchronously
         * and on slow devices we may indeed observe a device that no longer
         * exists.
         *
         * Similar debug + continue pattern repeats in all the udev calls in
         * this function. Related to LP: #1881209 */
        if (device == NULL) {
            debug("cannot find device from syspath %s", path);
            continue;
        }
        /* If we are able to query if the device has a current tag,
         * do so and if there are no current tags, continue to prevent
         * allowing assigned devices to the cgroup - this has the net
         * desired effect of not re-creating device cgroups that were
         * previously created/setup but should no longer be setup due
         * to interface disconnection, etc. */
        if (__sc_udev_device_has_current_tag != NULL) {
            if (__sc_udev_device_has_current_tag(device, udev_tag) <= 0) {
                debug("device %s has no matching current tag", path);
                udev_device_unref(device);
                continue;
            }
            debug("device %s has matching current tag", path);
        }

        if (cgroup == NULL) {
            /* initialize cgroup wrapper only when we are sure that there are
             * devices assigned to this snap */
            cgroup = sc_device_cgroup_new(security_tag, 0);
            /* Setup the device group access control list */
            sc_udev_setup_acls_common(cgroup);
        }
        sc_udev_allow_assigned_device(cgroup, device);
        udev_device_unref(device);
    }
    if (cgroup != NULL) {
        /* Move ourselves to the device cgroup */
        sc_device_cgroup_attach_pid(cgroup, getpid());
        debug("associated snap application process %i with device cgroup %s", getpid(), security_tag);
    } else {
        debug("no devices tagged with %s, skipping device cgroup setup", udev_tag);
    }
}
