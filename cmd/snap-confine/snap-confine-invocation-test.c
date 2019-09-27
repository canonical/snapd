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

#include "snap-confine-invocation.h"
#include "snap-confine-args.h"
#include "snap-confine-invocation.c"

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/test-utils.h"

#include <stdarg.h>

#include <glib.h>

static struct sc_args *test_prepare_args(const char *base, const char *tag) {
    struct sc_args *args = NULL;
    sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    int argc;
    char **argv;

    test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine", "--base", (base != NULL) ? base : "core",
                   (tag != NULL) ? tag : "snap.foo.app", "/usr/lib/snapd/snap-exec", NULL);
    args = sc_nonfatal_parse_args(&argc, &argv, &err);
    g_assert_null(err);
    g_assert_nonnull(args);

    return args;
}

static void test_sc_invocation_basic(void) {
    struct sc_args *args SC_CLEANUP(sc_cleanup_args) = test_prepare_args("core", NULL);

    sc_invocation inv SC_CLEANUP(sc_cleanup_invocation);
    ;
    sc_init_invocation(&inv, args, "foo");

    g_assert_cmpstr(inv.base_snap_name, ==, "core");
    g_assert_cmpstr(inv.executable, ==, "/usr/lib/snapd/snap-exec");
    g_assert_cmpstr(inv.orig_base_snap_name, ==, "core");
    g_assert_cmpstr(inv.rootfs_dir, ==, SNAP_MOUNT_DIR "/core/current");
    g_assert_cmpstr(inv.security_tag, ==, "snap.foo.app");
    g_assert_cmpstr(inv.snap_instance, ==, "foo");
    g_assert_cmpstr(inv.snap_name, ==, "foo");
    g_assert_false(inv.classic_confinement);
    /* derived later */
    g_assert_false(inv.is_normal_mode);
}

static void test_sc_invocation_instance_key(void) {
    struct sc_args *args SC_CLEANUP(sc_cleanup_args) = test_prepare_args("core", "snap.foo_bar.app");

    sc_invocation inv SC_CLEANUP(sc_cleanup_invocation);
    ;
    sc_init_invocation(&inv, args, "foo_bar");

    // Check the error that we've got
    g_assert_cmpstr(inv.snap_instance, ==, "foo_bar");
    g_assert_cmpstr(inv.snap_name, ==, "foo");
    g_assert_cmpstr(inv.orig_base_snap_name, ==, "core");
    g_assert_cmpstr(inv.security_tag, ==, "snap.foo_bar.app");
    g_assert_cmpstr(inv.executable, ==, "/usr/lib/snapd/snap-exec");
    g_assert_false(inv.classic_confinement);
    g_assert_cmpstr(inv.rootfs_dir, ==, SNAP_MOUNT_DIR "/core/current");
    g_assert_cmpstr(inv.base_snap_name, ==, "core");
    /* derived later */
    g_assert_false(inv.is_normal_mode);
}

static void test_sc_invocation_base_name(void) {
    struct sc_args *args SC_CLEANUP(sc_cleanup_args) = test_prepare_args("base-snap", NULL);

    sc_invocation inv SC_CLEANUP(sc_cleanup_invocation);
    sc_init_invocation(&inv, args, "foo");

    g_assert_cmpstr(inv.base_snap_name, ==, "base-snap");
    g_assert_cmpstr(inv.executable, ==, "/usr/lib/snapd/snap-exec");
    g_assert_cmpstr(inv.orig_base_snap_name, ==, "base-snap");
    g_assert_cmpstr(inv.rootfs_dir, ==, SNAP_MOUNT_DIR "/base-snap/current");
    g_assert_cmpstr(inv.security_tag, ==, "snap.foo.app");
    g_assert_cmpstr(inv.snap_instance, ==, "foo");
    g_assert_cmpstr(inv.snap_name, ==, "foo");
    g_assert_false(inv.classic_confinement);
    /* derived later */
    g_assert_false(inv.is_normal_mode);
}

static void test_sc_invocation_bad_instance_name(void) {
    struct sc_args *args SC_CLEANUP(sc_cleanup_args) = test_prepare_args(NULL, NULL);

    if (g_test_subprocess()) {
        sc_invocation inv SC_CLEANUP(sc_cleanup_invocation) = {0};
        sc_init_invocation(&inv, args, "foo_bar_bar_bar");
        return;
    }

    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("snap instance name can contain only one underscore\n");
}

static void test_sc_invocation_classic(void) {
    struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;
    sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
    int argc;
    char **argv = NULL;

    test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine", "--classic", "--base", "core", "snap.foo-classic.app",
                   "/usr/lib/snapd/snap-exec", NULL);
    args = sc_nonfatal_parse_args(&argc, &argv, &err);
    g_assert_null(err);
    g_assert_nonnull(args);

    sc_invocation inv SC_CLEANUP(sc_cleanup_invocation) = {0};
    sc_init_invocation(&inv, args, "foo-classic");

    g_assert_cmpstr(inv.base_snap_name, ==, "core");
    g_assert_cmpstr(inv.executable, ==, "/usr/lib/snapd/snap-exec");
    g_assert_cmpstr(inv.orig_base_snap_name, ==, "core");
    g_assert_cmpstr(inv.rootfs_dir, ==, SNAP_MOUNT_DIR "/core/current");
    g_assert_cmpstr(inv.security_tag, ==, "snap.foo-classic.app");
    g_assert_cmpstr(inv.snap_instance, ==, "foo-classic");
    g_assert_cmpstr(inv.snap_name, ==, "foo-classic");
    g_assert_true(inv.classic_confinement);
}

static void test_sc_invocation_tag_name_mismatch(void) {
    struct sc_args *args SC_CLEANUP(sc_cleanup_args) = test_prepare_args("core", "snap.foo.app");

    if (g_test_subprocess()) {
        sc_invocation inv SC_CLEANUP(sc_cleanup_invocation);
        ;
        sc_init_invocation(&inv, args, "foo-not-foo");
        return;
    }

    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("security tag snap.foo.app not allowed\n");
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/invocation/bad_instance_name", test_sc_invocation_bad_instance_name);
    g_test_add_func("/invocation/base_name", test_sc_invocation_base_name);
    g_test_add_func("/invocation/basic", test_sc_invocation_basic);
    g_test_add_func("/invocation/classic", test_sc_invocation_classic);
    g_test_add_func("/invocation/instance_key", test_sc_invocation_instance_key);
    g_test_add_func("/invocation/tag_name_mismatch", test_sc_invocation_tag_name_mismatch);
}
