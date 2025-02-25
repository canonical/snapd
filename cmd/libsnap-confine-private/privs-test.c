/*
 * Copyright (C) 2017 Canonical Ltd
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

#include "privs.h"
#include "privs.c"

#include <sys/capability.h>

#include <glib.h>

// Test that dropping permissions really works
static void test_sc_privs_drop(void) {
    /* expecting a file capability */
    cap_t start SC_CLEANUP(sc_cleanup_cap_t) = cap_get_proc();
    g_assert_nonnull(start);

    cap_t ref SC_CLEANUP(sc_cleanup_cap_t) = cap_init();
    g_assert_nonnull(ref);
    cap_value_t alarm = CAP_WAKE_ALARM;
    g_assert_cmpint(cap_set_flag(ref, CAP_INHERITABLE, 1, &alarm, CAP_SET), ==, 0);
    cap_value_t net_raw = CAP_NET_RAW;
    g_assert_cmpint(cap_set_flag(ref, CAP_PERMITTED, 1, &net_raw, CAP_SET), ==, 0);

    if (cap_compare(start, ref) != 0) {
        g_test_skip("run this test after 'sudo setcap cap_net_raw=p <unit-test-file>'");
        return;
    }

    if (g_test_subprocess()) {
        /* gtest reexecs itself, hence we retain CAP_NET_RAW even though it's
         * not in inherited set */
        g_assert_cmpint(cap_compare(start, ref), ==, 0);

        /* drop privileges */
        sc_privs_drop();

        cap_t working SC_CLEANUP(sc_cleanup_cap_t) = cap_get_proc();
        g_assert_nonnull(working);

        cap_t ref SC_CLEANUP(sc_cleanup_cap_t) = cap_init();
        g_assert_cmpint(cap_compare(working, ref), ==, 0);

        // We don't have any supplementary groups.
        gid_t groups[2];
        int num_groups = getgroups(1, groups);
        g_assert_cmpint(num_groups, <=, 1);
        if (num_groups == 1) {
            g_assert_cmpint(groups[0], ==, getgid());
        }

        // All done.
        return;
    }
    g_test_trap_subprocess(NULL, 0, G_TEST_SUBPROCESS_INHERIT_STDERR);
    g_test_trap_assert_passed();
}

static void test_sc_privs_cleanup(void) {
    cap_t start SC_CLEANUP(sc_cleanup_cap_t) = cap_get_proc();
    g_assert_nonnull(start);

    char *text SC_CLEANUP(sc_cleanup_cap_str) = cap_to_text(start, NULL);
    g_assert_nonnull(text);

    cap_t working SC_CLEANUP(sc_cleanup_cap_t) = cap_init();
    g_assert_nonnull(working);
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/privs/sc_privs_drop", test_sc_privs_drop);
    g_test_add_func("/privs/sc_cleanup_cap", test_sc_privs_cleanup);
}
