/*
 * Copyright (C) 2016 Canonical Ltd
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

#include "error.h"
#include "error.c"

#include <errno.h>
#include <glib.h>

static void test_sc_error_init(void) {
    struct sc_error *err;
    // Create an error
    err = sc_error_init("domain", 42, "printer is on %s", "fire");
    g_assert_nonnull(err);
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);

    // Inspect the exposed attributes
    g_assert_cmpstr(sc_error_domain(err), ==, "domain");
    g_assert_cmpint(sc_error_code(err), ==, 42);
    g_assert_cmpstr(sc_error_msg(err), ==, "printer is on fire");
}

static void test_sc_error_init_from_errno(void) {
    struct sc_error *err;
    // Create an error
    err = sc_error_init_from_errno(ENOENT, "printer is on %s", "fire");
    g_assert_nonnull(err);
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);

    // Inspect the exposed attributes
    g_assert_cmpstr(sc_error_domain(err), ==, SC_ERRNO_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, ENOENT);
    g_assert_cmpstr(sc_error_msg(err), ==, "printer is on fire");
}

static void test_sc_error_init_simple(void) {
    struct sc_error *err;
    // Create an error
    err = sc_error_init_simple("hello %s", "errors");
    g_assert_nonnull(err);
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);

    // Inspect the exposed attributes
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, 0);
    g_assert_cmpstr(sc_error_msg(err), ==, "hello errors");
}

static void test_sc_error_init_api_misuse(void) {
    struct sc_error *err;
    // Create an error
    err = sc_error_init_api_misuse("foo cannot be %d", 42);
    g_assert_nonnull(err);
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);

    // Inspect the exposed attributes
    g_assert_cmpstr(sc_error_domain(err), ==, SC_LIBSNAP_DOMAIN);
    g_assert_cmpint(sc_error_code(err), ==, SC_API_MISUSE);
    g_assert_cmpstr(sc_error_msg(err), ==, "foo cannot be 42");
}

static void test_sc_error_cleanup(void) {
    // Check that sc_error_cleanup() is safe to use.

    // Cleanup is safe on NULL errors.
    struct sc_error *err = NULL;
    sc_cleanup_error(&err);

    // Cleanup is safe on non-NULL errors.
    err = sc_error_init("domain", 123, "msg");
    g_assert_nonnull(err);
    sc_cleanup_error(&err);
    g_assert_null(err);
}

static void test_sc_error_domain__NULL(void) {
    // Check that sc_error_domain() dies if called with NULL error.
    if (g_test_subprocess()) {
        // NOTE: the code below fools gcc 5.4 but your mileage may vary.
        struct sc_error *err = NULL;
        const char *domain = sc_error_domain(err);
        (void)(domain);
        g_test_message("expected not to reach this place");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot obtain error domain from NULL error\n");
}

static void test_sc_error_code__NULL(void) {
    // Check that sc_error_code() dies if called with NULL error.
    if (g_test_subprocess()) {
        // NOTE: the code below fools gcc 5.4 but your mileage may vary.
        struct sc_error *err = NULL;
        int code = sc_error_code(err);
        (void)(code);
        g_test_message("expected not to reach this place");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot obtain error code from NULL error\n");
}

static void test_sc_error_msg__NULL(void) {
    // Check that sc_error_msg() dies if called with NULL error.
    if (g_test_subprocess()) {
        // NOTE: the code below fools gcc 5.4 but your mileage may vary.
        struct sc_error *err = NULL;
        const char *msg = sc_error_msg(err);
        (void)(msg);
        g_test_message("expected not to reach this place");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot obtain error message from NULL error\n");
}

static void test_sc_die_on_error__NULL(void) {
    // Check that sc_die_on_error() does nothing if called with NULL error.
    if (g_test_subprocess()) {
        sc_die_on_error(NULL);
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_passed();
}

static void test_sc_die_on_error__regular(void) {
    // Check that sc_die_on_error() dies if called with an error.
    if (g_test_subprocess()) {
        struct sc_error *err = sc_error_init("domain", 42, "just testing");
        sc_die_on_error(err);
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("just testing\n");
}

static void test_sc_die_on_error__errno(void) {
    // Check that sc_die_on_error() dies if called with an errno-based error.
    if (g_test_subprocess()) {
        struct sc_error *err = sc_error_init_from_errno(ENOENT, "just testing");
        sc_die_on_error(err);
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("just testing: No such file or directory\n");
}

static void test_sc_error_forward__nothing(void) {
    // Check that forwarding NULL does exactly that.
    struct sc_error *recipient = (void *)0xDEADBEEF;
    struct sc_error *err = NULL;
    int rc;
    rc = sc_error_forward(&recipient, err);
    g_assert_null(recipient);
    g_assert_cmpint(rc, ==, 0);
}

static void test_sc_error_forward__something_somewhere(void) {
    // Check that forwarding a real error works OK.
    struct sc_error *recipient = NULL;
    struct sc_error *err = sc_error_init("domain", 42, "just testing");
    int rc;
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);
    g_assert_nonnull(err);
    rc = sc_error_forward(&recipient, err);
    g_assert_nonnull(recipient);
    g_assert_cmpint(rc, ==, -1);
}

static void test_sc_error_forward__something_nowhere(void) {
    // Check that forwarding a real error nowhere calls die()
    if (g_test_subprocess()) {
        // NOTE: the code below fools gcc 5.4 but your mileage may vary.
        struct sc_error **err_ptr = NULL;
        struct sc_error *err = sc_error_init("domain", 42, "just testing");
        g_test_queue_destroy((GDestroyNotify)sc_error_free, err);
        g_assert_nonnull(err);
        sc_error_forward(err_ptr, err);
        g_test_message("expected not to reach this place");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("just testing\n");
}

static void test_sc_error_match__typical(void) {
    // NULL error doesn't match anything.
    g_assert_false(sc_error_match(NULL, "domain", 42));

    // Non-NULL error matches if domain and code both match.
    struct sc_error *err = sc_error_init("domain", 42, "just testing");
    g_test_queue_destroy((GDestroyNotify)sc_error_free, err);
    g_assert_true(sc_error_match(err, "domain", 42));
    g_assert_false(sc_error_match(err, "domain", 1));
    g_assert_false(sc_error_match(err, "other-domain", 42));
    g_assert_false(sc_error_match(err, "other-domain", 1));
}

static void test_sc_error_match__NULL_domain(void) {
    // Using a NULL domain is a fatal bug.
    if (g_test_subprocess()) {
        // NOTE: the code below fools gcc 5.4 but your mileage may vary.
        struct sc_error *err = NULL;
        const char *domain = NULL;
        g_assert_false(sc_error_match(err, domain, 42));
        g_test_message("expected not to reach this place");
        g_test_fail();
        return;
    }
    g_test_trap_subprocess(NULL, 0, 0);
    g_test_trap_assert_failed();
    g_test_trap_assert_stderr("cannot match error to a NULL domain\n");
}

static void __attribute__((constructor)) init(void) {
    g_test_add_func("/error/sc_error_init", test_sc_error_init);
    g_test_add_func("/error/sc_error_init_from_errno", test_sc_error_init_from_errno);
    g_test_add_func("/error/sc_error_init_simple", test_sc_error_init_simple);
    g_test_add_func("/error/sc_error_init_api_misue", test_sc_error_init_api_misuse);
    g_test_add_func("/error/sc_error_cleanup", test_sc_error_cleanup);
    g_test_add_func("/error/sc_error_domain/NULL", test_sc_error_domain__NULL);
    g_test_add_func("/error/sc_error_code/NULL", test_sc_error_code__NULL);
    g_test_add_func("/error/sc_error_msg/NULL", test_sc_error_msg__NULL);
    g_test_add_func("/error/sc_die_on_error/NULL", test_sc_die_on_error__NULL);
    g_test_add_func("/error/sc_die_on_error/regular", test_sc_die_on_error__regular);
    g_test_add_func("/error/sc_die_on_error/errno", test_sc_die_on_error__errno);
    g_test_add_func("/error/sc_error_formward/nothing", test_sc_error_forward__nothing);
    g_test_add_func("/error/sc_error_formward/something_somewhere", test_sc_error_forward__something_somewhere);
    g_test_add_func("/error/sc_error_formward/something_nowhere", test_sc_error_forward__something_nowhere);
    g_test_add_func("/error/sc_error_match/typical", test_sc_error_match__typical);
    g_test_add_func("/error/sc_error_match/NULL_domain", test_sc_error_match__NULL_domain);
}
