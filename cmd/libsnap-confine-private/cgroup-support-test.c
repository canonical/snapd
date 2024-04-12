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

#include "cgroup-support.c"

#include <fcntl.h>
#include <glib.h>
#include <glib/gstdio.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/test-utils.h"
#include "cgroup-support.h"

static void sc_set_self_cgroup_path(const char *mock);

static void sc_set_cgroup_root(const char *mock) { cgroup_dir = mock; }

typedef struct _cgroupv2_is_tracking_fixture {
    char *self_cgroup;
    char *root;
} cgroupv2_is_tracking_fixture;

static void cgroupv2_is_tracking_set_up(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    GError *err = NULL;
    int fd = g_file_open_tmp("s-c-unit-is-tracking-self-group.XXXXXX", &fixture->self_cgroup, &err);
    g_assert_no_error(err);
    g_assert_cmpint(fd, >=, 0);
    g_close(fd, &err);
    g_assert_no_error(err);
    sc_set_self_cgroup_path(fixture->self_cgroup);

    fixture->root = g_dir_make_tmp("s-c-unit-test-root.XXXXXX", &err);
    sc_set_cgroup_root(fixture->root);
}

static void cgroupv2_is_tracking_tear_down(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    GError *err = NULL;

    sc_set_self_cgroup_path("/proc/self/cgroup");
    /* mocked file may have been removed by the test */
    (void)g_remove(fixture->self_cgroup);
    g_free(fixture->self_cgroup);

    sc_set_cgroup_root("/sys/fs/cgroup");
    char *cmd = g_strdup_printf("rm -rf %s", fixture->root);
    g_debug("cleanup command: %s", cmd);
    g_spawn_command_line_sync(cmd, NULL, NULL, NULL, &err);
    g_free(cmd);
    g_assert_no_error(err);
    g_free(fixture->root);
}

static void _test_sc_cgroupv2_is_tracking_happy(cgroupv2_is_tracking_fixture *fixture) {
    /* there exist 3 groups with processes from a given snap */
    const char *dirs[] = {
        "/foo/bar/baz/snap.foo.app.1234-1234.scope",
        "/foo/bar/snap.foo.app.1111-1111.scope",
        "/foo/bar/bad",
        "/system.slice/snap.foo.bar.service",
        "/user/slice/other/app",
    };

    for (size_t i = 0; i < sizeof dirs / sizeof dirs[0]; i++) {
        char *np = g_build_filename(fixture->root, dirs[i], NULL);
        int ret = g_mkdir_with_parents(np, 0755);
        g_assert_cmpint(ret, ==, 0);
        g_free(np);
    }

    bool is_tracking = sc_cgroup_v2_is_tracking_snap("foo");
    g_assert_true(is_tracking);
}

static void test_sc_cgroupv2_is_tracking_happy_scope(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.1234-1234.scope", -1, NULL));

    _test_sc_cgroupv2_is_tracking_happy(fixture);
}

static void test_sc_cgroupv2_is_tracking_happy_service(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/system.slice/snap.foo.svc.service", -1, NULL));

    _test_sc_cgroupv2_is_tracking_happy(fixture);
}

static void test_sc_cgroupv2_is_tracking_just_own_group(cgroupv2_is_tracking_fixture *fixture,
                                                        gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.1234-1234.scope", -1, NULL));

    /* our group is the only one for this snap */
    const char *dirs[] = {
        "/foo/bar/baz/snap.foo.app.1234-1234.scope",
        "/foo/bar/bad",
        "/system.slice/some/app/other",
        "/user/slice/other/app",
    };

    for (size_t i = 0; i < sizeof dirs / sizeof dirs[0]; i++) {
        char *np = g_build_filename(fixture->root, dirs[i], NULL);
        int ret = g_mkdir_with_parents(np, 0755);
        g_assert_cmpint(ret, ==, 0);
        g_free(np);
    }

    bool is_tracking = sc_cgroup_v2_is_tracking_snap("foo");
    /* our own group is skipped */
    g_assert_false(is_tracking);
}

static void test_sc_cgroupv2_is_tracking_other_snaps(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.1234-1234.scope", -1, NULL));

    /* our group is the only one for this snap */
    const char *dirs[] = {
        "/foo/bar/baz/snap.other.app.1234-1234.scope",
        "/foo/bar/bad",
        "/system.slice/some/app/snap.one-more.app.service",
        "/user/slice/other/app",
    };

    for (size_t i = 0; i < sizeof dirs / sizeof dirs[0]; i++) {
        char *np = g_build_filename(fixture->root, dirs[i], NULL);
        int ret = g_mkdir_with_parents(np, 0755);
        g_assert_cmpint(ret, ==, 0);
        g_free(np);
    }

    bool is_tracking = sc_cgroup_v2_is_tracking_snap("foo");
    /* our own group is skipped */
    g_assert_false(is_tracking);
}

static void test_sc_cgroupv2_is_tracking_no_dirs(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.scope", -1, NULL));

    bool is_tracking = sc_cgroup_v2_is_tracking_snap("foo");
    g_assert_false(is_tracking);
}

static void test_sc_cgroupv2_is_tracking_bad_self_group(cgroupv2_is_tracking_fixture *fixture,
                                                        gconstpointer user_data) {
    /* trigger a failure in own group handling */
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "", -1, NULL));

    if (g_test_subprocess()) {
        sc_cgroup_v2_is_tracking_snap("foo");
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot obtain own cgroup v2 group path\n");
}

static void test_sc_cgroupv2_is_tracking_bad_nesting(cgroupv2_is_tracking_fixture *fixture, gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.scope", -1, NULL));

    /* create a hierarchy so deep that it triggers the nesting error */
    char *prev_path = g_build_filename(fixture->root, NULL);
    for (size_t i = 0; i < max_traversal_depth; i++) {
        char *np = g_build_filename(prev_path, "nested", NULL);
        int ret = g_mkdir_with_parents(np, 0755);
        g_assert_cmpint(ret, ==, 0);
        g_free(prev_path);
        prev_path = np;
    }
    g_free(prev_path);

    if (g_test_subprocess()) {
        sc_cgroup_v2_is_tracking_snap("foo");
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot traverse cgroups hierarchy deeper than 32 levels\n");
}

static void test_sc_cgroupv2_is_tracking_dir_permissions(cgroupv2_is_tracking_fixture *fixture,
                                                         gconstpointer user_data) {
    if (geteuid() == 0) {
        g_test_skip("the test will not work when running as root");
        return;
    }
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.1234-1234.scope", -1, NULL));

    /* there exist 2 groups with processes from a given snap */
    const char *dirs[] = {
        "/foo/bar/bad",
        "/foo/bar/bad/badperm",
    };
    for (size_t i = 0; i < sizeof dirs / sizeof dirs[0]; i++) {
        int mode = 0755;
        if (g_str_has_suffix(dirs[i], "/badperm")) {
            mode = 0000;
        }
        char *np = g_build_filename(fixture->root, dirs[i], NULL);
        int ret = g_mkdir_with_parents(np, mode);
        g_assert_cmpint(ret, ==, 0);
        g_free(np);
    }

    /* dies when hitting an error traversing the hierarchy */
    if (g_test_subprocess()) {
        sc_cgroup_v2_is_tracking_snap("foo");
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot open directory entry \"badperm\": Permission denied\n");
}

static void test_sc_cgroupv2_is_tracking_no_cgroup_root(cgroupv2_is_tracking_fixture *fixture,
                                                        gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, "0::/foo/bar/baz/snap.foo.app.1234-1234.scope", -1, NULL));

    sc_set_cgroup_root("/does/not/exist");

    // does not die when cgroup root is not present
    bool is_tracking = sc_cgroup_v2_is_tracking_snap("foo");
    g_assert_false(is_tracking);
}

static void sc_set_self_cgroup_path(const char *mock) { self_cgroup = mock; }

typedef struct _cgroupv2_own_group_fixture {
    char *self_cgroup;
} cgroupv2_own_group_fixture;

static void cgroupv2_own_group_set_up(cgroupv2_own_group_fixture *fixture, gconstpointer user_data) {
    GError *err = NULL;
    int fd = g_file_open_tmp("s-c-unit-test.XXXXXX", &fixture->self_cgroup, &err);
    g_assert_no_error(err);
    g_close(fd, &err);
    g_assert_no_error(err);
    sc_set_self_cgroup_path(fixture->self_cgroup);
}

static void cgroupv2_own_group_tear_down(cgroupv2_own_group_fixture *fixture, gconstpointer user_data) {
    sc_set_self_cgroup_path("/proc/self/cgroup");
    if (g_remove(fixture->self_cgroup) < 0) {
        /* test may have removed the file */
        g_assert_cmpint(errno, ==, ENOENT);
    }
    g_free(fixture->self_cgroup);
}

static void test_sc_cgroupv2_own_group_path_simple_happy_scope(cgroupv2_own_group_fixture *fixture,
                                                               gconstpointer user_data) {
    char *p SC_CLEANUP(sc_cleanup_string) = NULL;
    g_assert_true(g_file_set_contents(fixture->self_cgroup, (char *)user_data, -1, NULL));
    p = sc_cgroup_v2_own_path_full();
    g_assert_cmpstr(p, ==, "/foo/bar/baz.slice/snap.foo.bar.1234-1234.scope");
}

static void test_sc_cgroupv2_own_group_path_simple_happy_service(cgroupv2_own_group_fixture *fixture,
                                                                 gconstpointer user_data) {
    char *p SC_CLEANUP(sc_cleanup_string) = NULL;
    g_assert_true(g_file_set_contents(fixture->self_cgroup, (char *)user_data, -1, NULL));
    p = sc_cgroup_v2_own_path_full();
    g_assert_cmpstr(p, ==, "/system.slice/snap.foo.bar.service");
}

static void test_sc_cgroupv2_own_group_path_empty(cgroupv2_own_group_fixture *fixture, gconstpointer user_data) {
    char *p SC_CLEANUP(sc_cleanup_string) = NULL;
    g_assert_true(g_file_set_contents(fixture->self_cgroup, (char *)user_data, -1, NULL));
    p = sc_cgroup_v2_own_path_full();
    g_assert_null(p);
}

static void _test_sc_cgroupv2_own_group_path_die_with_message(const char *msg) {
    if (g_test_subprocess()) {
        char *p = NULL;
        p = sc_cgroup_v2_own_path_full();
        /* not reached */
        sc_cleanup_string(&p);
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr(msg);
}

static void test_sc_cgroupv2_own_group_path_die(cgroupv2_own_group_fixture *fixture, gconstpointer user_data) {
    g_assert_true(g_file_set_contents(fixture->self_cgroup, (char *)user_data, -1, NULL));
    _test_sc_cgroupv2_own_group_path_die_with_message("unexpected content of group entry 0::\n");
}

static void test_sc_cgroupv2_own_group_path_no_file(cgroupv2_own_group_fixture *fixture, gconstpointer user_data) {
    /* make sure that the file is removed if it exists */
    (void)g_remove(fixture->self_cgroup);
    _test_sc_cgroupv2_own_group_path_die_with_message("cannot open *\n");
}

static void test_sc_cgroupv2_own_group_path_permission(cgroupv2_own_group_fixture *fixture, gconstpointer user_data) {
    if (geteuid() == 0) {
        g_test_skip("the test will not work when running as root");
        return;
    }
    int ret = g_chmod(fixture->self_cgroup, 0000);
    g_assert_cmpint(ret, ==, 0);
    _test_sc_cgroupv2_own_group_path_die_with_message("cannot open *: Permission denied\n");
}

static void __attribute__((constructor)) init(void) {
    g_test_add("/cgroup/v2/own_path_full_newline", cgroupv2_own_group_fixture,
               "0::/foo/bar/baz.slice/snap.foo.bar.1234-1234.scope\n", cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_simple_happy_scope, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_no_newline", cgroupv2_own_group_fixture,
               "0::/foo/bar/baz.slice/snap.foo.bar.1234-1234.scope", cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_simple_happy_scope, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_firstline", cgroupv2_own_group_fixture,
               "0::/foo/bar/baz.slice/snap.foo.bar.1234-1234.scope\n"
               "0::/bad\n",
               cgroupv2_own_group_set_up, test_sc_cgroupv2_own_group_path_simple_happy_scope,
               cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_ignore_non_unified", cgroupv2_own_group_fixture,
               "1::/ignored\n"
               "0::/foo/bar/baz.slice/snap.foo.bar.1234-1234.scope\n",
               cgroupv2_own_group_set_up, test_sc_cgroupv2_own_group_path_simple_happy_scope,
               cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_service", cgroupv2_own_group_fixture,
               "0::/system.slice/snap.foo.bar.service\n", cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_simple_happy_service, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_empty", cgroupv2_own_group_fixture, "", cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_empty, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_not_found", cgroupv2_own_group_fixture,
               /* missing 0:: group */
               "1::/ignored\n"
               "2::/foo/bar/baz.slice\n",
               cgroupv2_own_group_set_up, test_sc_cgroupv2_own_group_path_empty, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_die", cgroupv2_own_group_fixture, "0::", cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_die, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_no_file", cgroupv2_own_group_fixture, NULL, cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_no_file, cgroupv2_own_group_tear_down);
    g_test_add("/cgroup/v2/own_path_full_permission", cgroupv2_own_group_fixture, NULL, cgroupv2_own_group_set_up,
               test_sc_cgroupv2_own_group_path_permission, cgroupv2_own_group_tear_down);

    g_test_add("/cgroup/v2/is_tracking_happy_scope", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_happy_scope, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_happy_service", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_happy_service, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_just_own", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_just_own_group, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_only_other_snaps", cgroupv2_is_tracking_fixture, NULL,
               cgroupv2_is_tracking_set_up, test_sc_cgroupv2_is_tracking_other_snaps, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_empty_groups", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_no_dirs, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_bad_self_group", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_bad_self_group, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_bad_dir_permissions", cgroupv2_is_tracking_fixture, NULL,
               cgroupv2_is_tracking_set_up, test_sc_cgroupv2_is_tracking_dir_permissions,
               cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_bad_nesting", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_bad_nesting, cgroupv2_is_tracking_tear_down);
    g_test_add("/cgroup/v2/is_tracking_no_cgroup_root", cgroupv2_is_tracking_fixture, NULL, cgroupv2_is_tracking_set_up,
               test_sc_cgroupv2_is_tracking_no_cgroup_root, cgroupv2_is_tracking_tear_down);
}
