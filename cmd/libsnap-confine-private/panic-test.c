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

#include "panic.h"
#include "panic.c"

#include <glib.h>

static void test_panic(void) {
    if (g_test_subprocess()) {
        errno = 0;
        sc_panic("death message");
        g_test_message("expected die not to return");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("death message\n");
}

static void test_panic_with_errno(void) {
    if (g_test_subprocess()) {
        errno = EPERM;
        sc_panic("death message");
        g_test_message("expected die not to return");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("death message: Operation not permitted\n");
}

static void custom_panic_msg(const char *fmt, va_list ap, int errno_copy) {
    fprintf(stderr, "PANIC: ");
    vfprintf(stderr, fmt, ap);
    fprintf(stderr, " (errno: %d)", errno_copy);
    fprintf(stderr, "\n");
}

static void custom_panic_exit(void) {
    fprintf(stderr, "EXITING\n");
    exit(2);
}

static void test_panic_customization(void) {
    if (g_test_subprocess()) {
        sc_set_panic_msg_fn(custom_panic_msg);
        sc_set_panic_exit_fn(custom_panic_exit);
        errno = 123;
        sc_panic("death message");
        g_test_message("expected die not to return");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr(
        "PANIC: death message (errno: 123)\n"
        "EXITING\n");
    // NOTE: g_test doesn't offer facilities to observe the exit code.
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/panic/panic", test_panic);
    g_test_add_func("/panic/panic_with_errno", test_panic_with_errno);
    g_test_add_func("/panic/panic_customization", test_panic_customization);
}
