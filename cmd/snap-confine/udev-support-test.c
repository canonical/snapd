/*
 * Copyright (C) 2026 Canonical Ltd
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

#include "udev-support.c"

#include <glib.h>

static void test_sc_security_to_udev_tag(void) {
    const char *security_tags[] = {
        "snap.foo.app",
        "snap.foo.hook.configure",
        "snap.foo+bar.hook.install",
        "snap.foo_instance+bar.hook.install",
    };
    const char *udev_tags[] = {
        "snap_foo_app",
        "snap_foo_hook_configure",
        "snap_foo__bar_hook_install",
        "snap_foo_instance__bar_hook_install",
    };

    for (size_t index = 0; index < G_N_ELEMENTS(security_tags); ++index) {
        char *tag SC_CLEANUP(sc_cleanup_string) = sc_security_to_udev_tag(security_tags[index]);
        g_assert_cmpstr(tag, ==, udev_tags[index]);
    }
}

static void test_sc_security_to_udev_tag_multiple_plus(void) {
    if (g_test_subprocess()) {
        char *tag SC_CLEANUP(sc_cleanup_string) = sc_security_to_udev_tag("snap.foo+bar+baz.hook.install");
        g_test_message("expected sc_security_to_udev_tag not to return");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("internal error: security tag contains more '+' than expected\n");
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/udev/security_to_udev_tag", test_sc_security_to_udev_tag);
    g_test_add_func("/udev/security_to_udev_tag_multiple_plus", test_sc_security_to_udev_tag_multiple_plus);
}
