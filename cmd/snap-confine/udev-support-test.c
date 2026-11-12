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

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/udev/security_to_udev_tag", test_sc_security_to_udev_tag);
}
