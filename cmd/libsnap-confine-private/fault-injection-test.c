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

#include "fault-injection.h"
#include "fault-injection.c"

#include <errno.h>
#include <glib.h>

static bool broken(struct sc_fault_state *state, void *ptr) { return true; }

static bool broken_alter_msg(struct sc_fault_state *state, void *ptr) {
    char **s = ptr;
    *s = "broken";
    return true;
}

static void test_fault_injection(void) {
    g_assert_false(sc_faulty("foo", NULL));

    sc_break("foo", broken);
    g_assert_true(sc_faulty("foo", NULL));

    sc_reset_faults();
    g_assert_false(sc_faulty("foo", NULL));

    const char *msg = NULL;
    if (!sc_faulty("foo", &msg)) {
        msg = "working";
    }
    g_assert_cmpstr(msg, ==, "working");

    sc_break("foo", broken_alter_msg);
    if (!sc_faulty("foo", &msg)) {
        msg = "working";
    }
    g_assert_cmpstr(msg, ==, "broken");
    sc_reset_faults();
}

static void __attribute__((constructor)) init(void) { g_test_add_func("/fault-injection", test_fault_injection); }
