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

#include "../libsnap-confine-private/test-utils.h"

#include <fcntl.h>
#include <glib.h>
#include <glib/gstdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "snap-device-helper.c"

#include "../libsnap-confine-private/device-cgroup-support.h"

typedef struct _sdh_test_fixture {
    char *sysroot;
} sdh_test_fixture;

static void mkdir_in_sysroot(sdh_test_fixture *fixture, const char *path) {
    char *p = g_build_filename(fixture->sysroot, path, NULL);
    g_assert(g_mkdir_with_parents(p, 0755) == 0);
    g_free(p);
}

static void symlink_in_sysroot(sdh_test_fixture *fixture, const char *from, const char *to) {
    g_debug("mock symlink from %s to %s", from, to);
    char *pfrom = g_build_filename(fixture->sysroot, from, NULL);
    g_assert(g_path_is_absolute(to) == FALSE);
    g_assert_cmpint(symlink(to, pfrom), ==, 0);
    g_free(pfrom);
}

static void sdh_test_set_up(sdh_test_fixture *fixture, gconstpointer user_data) {
    gchar *mock_dir = g_dir_make_tmp(NULL, NULL);
    g_assert_nonnull(mock_dir);

    fixture->sysroot = mock_dir;
    sysroot = mock_dir;

    char *sys_devices = g_build_filename(fixture->sysroot, "sys", "devices", NULL);
    g_assert(g_mkdir_with_parents(sys_devices, 0755) == 0);
    g_free(sys_devices);
    char *sys_class_block = g_build_filename(fixture->sysroot, "sys", "class", "block", NULL);
    g_assert(g_mkdir_with_parents(sys_class_block, 0755) == 0);
    g_free(sys_class_block);
    char *sys_class_other = g_build_filename(fixture->sysroot, "sys", "class", "other", NULL);
    g_assert(g_mkdir_with_parents(sys_class_other, 0755) == 0);
    g_free(sys_class_other);

    g_debug("mock sysroot dir: %s", mock_dir);
}

static void mocks_reset(void);

static void sdh_test_tear_down(sdh_test_fixture *fixture, gconstpointer user_data) {
    sysroot = "";
    if (!g_strcmp0(sysroot, "/")) {
        rm_rf_tmp(fixture->sysroot);
    }
    mocks_reset();
    g_free(fixture->sysroot);
}

static struct mocks {
    size_t cgroup_new_calls;
    void *new_ret;
    char *new_tag;
    int new_flags;

    size_t cgroup_allow_calls;
    size_t cgroup_deny_calls;
    int device_type;
    int device_major;
    int device_minor;
    int device_ret;

} mocks;

static void mocks_reset(void) {
    if (mocks.new_tag != NULL) {
        g_free(mocks.new_tag);
    }
    memset(&mocks, 0, sizeof(mocks));
}

/* mocked in test */
sc_device_cgroup *sc_device_cgroup_new(const char *security_tag, int flags) {
    g_debug("cgroup new called");
    mocks.cgroup_new_calls++;
    mocks.new_tag = g_strdup(security_tag);
    mocks.new_flags = flags;
    return (sc_device_cgroup *)mocks.new_ret;
}

int sc_device_cgroup_allow(sc_device_cgroup *self, int kind, int major, int minor) {
    mocks.cgroup_allow_calls++;
    mocks.device_type = kind;
    mocks.device_major = major;
    mocks.device_minor = minor;
    return 0;
}

int sc_device_cgroup_deny(sc_device_cgroup *self, int kind, int major, int minor) {
    mocks.cgroup_deny_calls++;
    mocks.device_type = kind;
    mocks.device_major = major;
    mocks.device_minor = minor;
    return 0;
}

struct sdh_test_data {
    char *action;
    // snap.foo.bar
    char *app;
    // snap_foo_bar
    char *mangled_appname;
};

static void test_sdh_action(sdh_test_fixture *fixture, gconstpointer test_data) {
    struct sdh_test_data *td = (struct sdh_test_data *)test_data;

    struct sdh_invocation inv_block = {
        .action = td->action,
        .tagname = td->mangled_appname,
        .devpath = "/devices/foo/block/sda/sda4",
        .majmin = "8:4",
    };

    mkdir_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4");
    symlink_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4/subsystem", "../../../../../class/block");

    int bogus = 0;
    /* make cgroup_device_new return a non-NULL */
    mocks.new_ret = &bogus;

    int ret = snap_device_helper_run(&inv_block);
    g_assert_cmpint(ret, ==, 0);
    g_assert_cmpint(mocks.cgroup_new_calls, ==, 1);
    if (g_strcmp0(td->action, "add") == 0 || g_strcmp0(td->action, "change") == 0) {
        g_assert_cmpint(mocks.cgroup_allow_calls, ==, 1);
        g_assert_cmpint(mocks.cgroup_deny_calls, ==, 0);
    } else if (g_strcmp0(td->action, "remove") == 0) {
        g_assert_cmpint(mocks.cgroup_allow_calls, ==, 0);
        g_assert_cmpint(mocks.cgroup_deny_calls, ==, 1);
    }
    g_assert_cmpint(mocks.device_major, ==, 8);
    g_assert_cmpint(mocks.device_minor, ==, 4);
    g_assert_cmpint(mocks.device_type, ==, S_IFBLK);
    g_assert_nonnull(mocks.new_tag);
    g_assert_nonnull(td->app);
    g_assert_cmpstr(mocks.new_tag, ==, td->app);
    g_assert_cmpint(mocks.new_flags, !=, 0);
    g_assert_cmpint(mocks.new_flags, ==, SC_DEVICE_CGROUP_FROM_EXISTING);

    g_debug("reset");
    mocks_reset();
    mocks.new_ret = &bogus;

    struct sdh_invocation inv_serial = {
        .action = td->action,
        .tagname = td->mangled_appname,
        .devpath = "/devices/foo/tty/ttyS0",
        .majmin = "6:64",
    };
    mkdir_in_sysroot(fixture, "/sys/devices/foo/tty/ttyS0");
    symlink_in_sysroot(fixture, "/sys/devices/foo/tty/ttyS0/subsystem", "../../../../class/other");
    ret = snap_device_helper_run(&inv_serial);
    g_assert_cmpint(ret, ==, 0);
    g_assert_cmpint(mocks.cgroup_new_calls, ==, 1);
    if (g_strcmp0(td->action, "add") == 0 || g_strcmp0(td->action, "change") == 0) {
        g_assert_cmpint(mocks.cgroup_allow_calls, ==, 1);
        g_assert_cmpint(mocks.cgroup_deny_calls, ==, 0);
    } else if (g_strcmp0(td->action, "remove") == 0) {
        g_assert_cmpint(mocks.cgroup_allow_calls, ==, 0);
        g_assert_cmpint(mocks.cgroup_deny_calls, ==, 1);
    }
    g_assert_cmpint(mocks.device_major, ==, 6);
    g_assert_cmpint(mocks.device_minor, ==, 64);
    g_assert_cmpint(mocks.device_type, ==, S_IFCHR);
    g_assert_nonnull(mocks.new_tag);
    g_assert_nonnull(td->app);
    g_assert_cmpstr(mocks.new_tag, ==, td->app);
    g_assert_cmpint(mocks.new_flags, !=, 0);
    g_assert_cmpint(mocks.new_flags, ==, SC_DEVICE_CGROUP_FROM_EXISTING);
}

static void test_sdh_action_nvme(sdh_test_fixture *fixture, gconstpointer test_data) {
    /* hierarchy from an actual system with a nvme disk */
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1");
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1p1");
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/ng0n1");
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/hwmon0");
    symlink_in_sysroot(fixture, "/sys//devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1/subsystem",
                       "../../../../../../../class/block");
    symlink_in_sysroot(fixture, "/sys//devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1p1/subsystem",
                       "../../../../../../../class/block");
    symlink_in_sysroot(fixture, "/sys//devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/subsystem",
                       "../../../../../../class/nvme");
    symlink_in_sysroot(fixture, "/sys//devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/ng0n1/subsystem",
                       "../../../../../../class/nvme-generic");
    symlink_in_sysroot(fixture, "/sys//devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/hwmon0/subsystem",
                       "../../../../../../class/hwmon");

    struct {
        const char *dev;
        const char *majmin;
        int expected_maj;
        int expected_min;
        int expected_type;
    } tcs[] = {
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1",
            .majmin = "259:0",
            .expected_maj = 259,
            .expected_min = 0,
            .expected_type = S_IFBLK,
        },
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1p1",
            .majmin = "259:1",
            .expected_maj = 259,
            .expected_min = 1,
            .expected_type = S_IFBLK,
        },
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0",
            .majmin = "242:0",
            .expected_maj = 242,
            .expected_min = 0,
            .expected_type = S_IFCHR,
        },
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/hwmon0",
            .majmin = "241:0",
            .expected_maj = 241,
            .expected_min = 0,
            .expected_type = S_IFCHR,
        },
    };

    int bogus = 0;

    for (size_t i = 0; i < sizeof(tcs) / sizeof(tcs[0]); i++) {
        mocks_reset();
        /* make cgroup_device_new return a non-NULL */
        mocks.new_ret = &bogus;

        struct sdh_invocation inv_block = {
            .action = "add",
            .tagname = "snap_foo_bar",
            .devpath = tcs[i].dev,
            .majmin = tcs[i].majmin,
        };
        int ret = snap_device_helper_run(&inv_block);
        g_assert_cmpint(ret, ==, 0);
        g_assert_cmpint(mocks.cgroup_new_calls, ==, 1);
        g_assert_cmpint(mocks.cgroup_allow_calls, ==, 1);
        g_assert_cmpint(mocks.cgroup_deny_calls, ==, 0);
        g_assert_cmpint(mocks.device_major, ==, tcs[i].expected_maj);
        g_assert_cmpint(mocks.device_minor, ==, tcs[i].expected_min);
        g_assert_cmpint(mocks.device_type, ==, tcs[i].expected_type);
        g_assert_cmpint(mocks.new_flags, !=, 0);
        g_assert_cmpint(mocks.new_flags, ==, SC_DEVICE_CGROUP_FROM_EXISTING);
    }
}

static void test_sdh_action_remove_fallback_devtype(sdh_test_fixture *fixture, gconstpointer test_data) {
    /* check that fallback guessing of device type if applied during remove action */
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1");
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1p1");
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/ng0n1");
    mkdir_in_sysroot(fixture, "/sys/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/hwmon0");
    mkdir_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4");
    mkdir_in_sysroot(fixture, "/sys//devices/pnp0/00:04/tty/ttyS0");

    struct {
        const char *dev;
        const char *majmin;
        int expected_maj;
        int expected_min;
        int expected_type;
    } tcs[] = {
        /* these device paths match the fallback pattern of block devices */
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1",
            .majmin = "259:0",
            .expected_maj = 259,
            .expected_min = 0,
            .expected_type = S_IFBLK,
        },
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/nvme0n1p1",
            .majmin = "259:1",
            .expected_maj = 259,
            .expected_min = 1,
            .expected_type = S_IFBLK,
        },
        {
            .dev = "/devices/foo/block/sda/sda4",
            .majmin = "8:0",
            .expected_maj = 8,
            .expected_min = 0,
            .expected_type = S_IFBLK,
        },
        /* these are treated as char devices */
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0",
            .majmin = "242:0",
            .expected_maj = 242,
            .expected_min = 0,
            .expected_type = S_IFCHR,
        },
        {
            .dev = "/devices/pci0000:00/0000:00:01.1/0000:01:00.0/nvme/nvme0/hwmon0",
            .majmin = "241:0",
            .expected_maj = 241,
            .expected_min = 0,
            .expected_type = S_IFCHR,
        },
        {
            .dev = "/devices/pnp0/00:04/tty/ttyS0",
            .majmin = "4:64",
            .expected_maj = 4,
            .expected_min = 64,
            .expected_type = S_IFCHR,
        },
    };

    int bogus = 0;

    for (size_t i = 0; i < sizeof(tcs) / sizeof(tcs[0]); i++) {
        mocks_reset();
        /* make cgroup_device_new return a non-NULL */
        mocks.new_ret = &bogus;

        struct sdh_invocation inv_block = {
            .action = "remove",
            .tagname = "snap_foo_bar",
            .devpath = tcs[i].dev,
            .majmin = tcs[i].majmin,
        };
        int ret = snap_device_helper_run(&inv_block);
        g_assert_cmpint(ret, ==, 0);
        g_assert_cmpint(mocks.cgroup_new_calls, ==, 1);
        g_assert_cmpint(mocks.cgroup_allow_calls, ==, 0);
        g_assert_cmpint(mocks.cgroup_deny_calls, ==, 1);
        g_assert_cmpint(mocks.device_major, ==, tcs[i].expected_maj);
        g_assert_cmpint(mocks.device_minor, ==, tcs[i].expected_min);
        g_assert_cmpint(mocks.device_type, ==, tcs[i].expected_type);
        g_assert_cmpint(mocks.new_flags, !=, 0);
        g_assert_cmpint(mocks.new_flags, ==, SC_DEVICE_CGROUP_FROM_EXISTING);
    }
}

static void run_sdh_die(const char *action, const char *tagname, const char *devpath, const char *majmin,
                        const char *msg) {
    struct sdh_invocation inv = {
        .action = action,
        .tagname = tagname,
        .devpath = devpath,
        .majmin = majmin,
    };
    if (g_test_subprocess()) {
        errno = 0;
        snap_device_helper_run(&inv);
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr(msg);
}

static void test_sdh_err_noappname(sdh_test_fixture *fixture, gconstpointer test_data) {
    // missing appname
    run_sdh_die("add", "", "/devices/foo/block/sda/sda4", "8:4", "malformed tag \"\"\n");
}

static void test_sdh_err_badappname(sdh_test_fixture *fixture, gconstpointer test_data) {
    // malformed appname
    run_sdh_die("add", "foo_bar", "/devices/foo/block/sda/sda4", "8:4", "malformed tag \"foo_bar\"\n");
}
static void test_sdh_err_nodevpath(sdh_test_fixture *fixture, gconstpointer test_data) {
    // missing devpath
    run_sdh_die("add", "snap_foo_bar", "", "8:4", "no or malformed devpath \"\"\n");
}

static void test_sdh_err_wrongdevmajorminor1(sdh_test_fixture *fixture, gconstpointer test_data) {
    // missing device major:minor numbers
    run_sdh_die("add", "snap_foo_bar", "/devices/foo/block/sda/sda4", "", "no or malformed major/minor \"\"\n");
}

static void test_sdh_err_wrongdevmajorminor2(sdh_test_fixture *fixture, gconstpointer test_data) {
    // too short major:minor numbers
    run_sdh_die("add", "snap_foo_bar", "/devices/foo/block/sda/sda4", "8", "no or malformed major/minor \"8\"\n");
}

static void test_sdh_err_wrongdevmajorminor_late1(sdh_test_fixture *fixture, gconstpointer test_data) {
    // mock enough to the major:minor extraction in the code
    mkdir_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4");
    symlink_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4/subsystem", "../../../../../class/block");

    // ensure mocked sc_device_cgroup_new() returns non-NULL
    int bogus = 0;
    mocks.new_ret = &bogus;

    // missing ":"
    run_sdh_die("add", "snap_foo_bar", "/devices/foo/block/sda/sda4", "100", "malformed major:minor string: 100\n");
}

static void test_sdh_err_wrongdevmajorminor_late2(sdh_test_fixture *fixture, gconstpointer test_data) {
    // mock enough to the major:minor extraction in the code
    mkdir_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4");
    symlink_in_sysroot(fixture, "/sys/devices/foo/block/sda/sda4/subsystem", "../../../../../class/block");

    // ensure mocked sc_device_cgroup_new() returns non-NULL
    int bogus = 0;
    mocks.new_ret = &bogus;

    // missing part after ":"
    run_sdh_die("add", "snap_foo_bar", "/devices/foo/block/sda/sda4", "88:", "malformed major:minor string: 88:\n");
}

static void test_sdh_err_badaction(sdh_test_fixture *fixture, gconstpointer test_data) {
    // bogus action
    run_sdh_die("badaction", "snap_foo_bar", "/devices/foo/block/sda/sda4", "8:4",
                "ERROR: unknown action \"badaction\"\n");
}

static void test_sdh_err_nosymlink_block(sdh_test_fixture *fixture, gconstpointer test_data) {
    // missing symlink
    run_sdh_die("add", "snap_foo_bar", "/devices/foo/block/sda/sda4", "8:4",
                "cannot read symlink */sys//devices/foo/block/sda/sda4/subsystem*\n");
}

static void test_sdh_err_nosymlink_char(sdh_test_fixture *fixture, gconstpointer test_data) {
    // missing symlink
    run_sdh_die("add", "snap_foo_bar", "/devices/pnp0/00:04/tty/ttyS0", "4:64",
                "cannot read symlink */sys//devices/pnp0/00:04/tty/ttyS0/subsystem*\n");
}

static void test_sdh_err_funtag1(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap___bar", "/devices/foo/block/sda/sda4", "8:4",
                "security tag \"snap._.bar\" for snap \"_\" is not valid\n");
}

static void test_sdh_err_funtag2(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap_foobar", "/devices/foo/block/sda/sda4", "8:4",
                "missing app name in tag \"snap_foobar\"\n");
}

static void test_sdh_err_funtag3(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap_", "/devices/foo/block/sda/sda4", "8:4", "tag \"snap_\" length 5 is incorrect\n");
}

static void test_sdh_err_funtag4(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap_foo_", "/devices/foo/block/sda/sda4", "8:4",
                "security tag \"snap.foo.\" for snap \"foo\" is not valid\n");
}

static void test_sdh_err_funtag5(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die(
        "add", "snap_thisisverylonginstancenameabovelengthlimit_instancekey_bar", "/devices/foo/block/sda/sda4", "8:4",
        "snap instance of tag \"snap_thisisverylonginstancenameabovelengthlimit_instancekey_bar\" is too long\n");
}

static void test_sdh_err_funtag6(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap__barbar", "/devices/foo/block/sda/sda4", "8:4",
                "missing snap name in tag \"snap__barbar\"\n");
}

static void test_sdh_err_funtag7(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap_barbarbarbar", "/devices/foo/block/sda/sda4", "8:4",
                "missing app name in tag \"snap_barbarbarbar\"\n");
}

static void test_sdh_err_funtag8(sdh_test_fixture *fixture, gconstpointer test_data) {
    run_sdh_die("add", "snap_#_barbar", "/devices/foo/block/sda/sda4", "8:4",
                "security tag \"snap.#.barbar\" for snap \"#\" is not valid\n");
}

static struct sdh_test_data add_data = {"add", "snap.foo.bar", "snap_foo_bar"};
static struct sdh_test_data change_data = {"change", "snap.foo.bar", "snap_foo_bar"};

static struct sdh_test_data remove_data = {"remove", "snap.foo.bar", "snap_foo_bar"};

static struct sdh_test_data instance_add_data = {"add", "snap.foo_bar.baz", "snap_foo_bar_baz"};

static struct sdh_test_data instance_change_data = {"change", "snap.foo_bar.baz", "snap_foo_bar_baz"};

static struct sdh_test_data instance_remove_data = {"remove", "snap.foo_bar.baz", "snap_foo_bar_baz"};

static struct sdh_test_data add_hook_data = {"add", "snap.foo.hook.configure", "snap_foo_hook_configure"};

static struct sdh_test_data instance_add_hook_data = {"add", "snap.foo_bar.hook.configure",
                                                      "snap_foo_bar_hook_configure"};

static struct sdh_test_data instance_add_instance_name_is_hook_data = {"add", "snap.foo_hook.hook.configure",
                                                                       "snap_foo_hook_hook_configure"};

static void __attribute__((constructor)) init(void) {
#define _test_add(_name, _data, _func) \
    g_test_add(_name, sdh_test_fixture, _data, sdh_test_set_up, _func, sdh_test_tear_down)

    _test_add("/snap-device-helper/add", &add_data, test_sdh_action);
    _test_add("/snap-device-helper/change", &change_data, test_sdh_action);
    _test_add("/snap-device-helper/remove", &remove_data, test_sdh_action);
    _test_add("/snap-device-helper/remove_fallback", NULL, test_sdh_action_remove_fallback_devtype);

    _test_add("/snap-device-helper/err/no-appname", NULL, test_sdh_err_noappname);
    _test_add("/snap-device-helper/err/bad-appname", NULL, test_sdh_err_badappname);
    _test_add("/snap-device-helper/err/no-devpath", NULL, test_sdh_err_nodevpath);
    _test_add("/snap-device-helper/err/wrong-devmajorminor1", NULL, test_sdh_err_wrongdevmajorminor1);
    _test_add("/snap-device-helper/err/wrong-devmajorminor2", NULL, test_sdh_err_wrongdevmajorminor2);
    _test_add("/snap-device-helper/err/wrong-devmajorminor_late1", NULL, test_sdh_err_wrongdevmajorminor_late1);
    _test_add("/snap-device-helper/err/wrong-devmajorminor_late2", NULL, test_sdh_err_wrongdevmajorminor_late2);
    _test_add("/snap-device-helper/err/bad-action", NULL, test_sdh_err_badaction);
    _test_add("/snap-device-helper/err/no-symlink-block", NULL, test_sdh_err_nosymlink_block);
    _test_add("/snap-device-helper/err/no-symlink-char", NULL, test_sdh_err_nosymlink_char);
    _test_add("/snap-device-helper/err/funtag1", NULL, test_sdh_err_funtag1);
    _test_add("/snap-device-helper/err/funtag2", NULL, test_sdh_err_funtag2);
    _test_add("/snap-device-helper/err/funtag3", NULL, test_sdh_err_funtag3);
    _test_add("/snap-device-helper/err/funtag4", NULL, test_sdh_err_funtag4);
    _test_add("/snap-device-helper/err/funtag5", NULL, test_sdh_err_funtag5);
    _test_add("/snap-device-helper/err/funtag6", NULL, test_sdh_err_funtag6);
    _test_add("/snap-device-helper/err/funtag7", NULL, test_sdh_err_funtag7);
    _test_add("/snap-device-helper/err/funtag8", NULL, test_sdh_err_funtag8);
    // parallel instances
    _test_add("/snap-device-helper/parallel/add", &instance_add_data, test_sdh_action);
    _test_add("/snap-device-helper/parallel/change", &instance_change_data, test_sdh_action);
    _test_add("/snap-device-helper/parallel/remove", &instance_remove_data, test_sdh_action);
    // hooks
    _test_add("/snap-device-helper/hook/add", &add_hook_data, test_sdh_action);
    _test_add("/snap-device-helper/hook/parallel/add", &instance_add_hook_data, test_sdh_action);
    _test_add("/snap-device-helper/hook-name-hook/parallel/add", &instance_add_instance_name_is_hook_data,
              test_sdh_action);

    _test_add("/snap-device-helper/nvme", NULL, test_sdh_action_nvme);
}
