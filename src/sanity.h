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

#ifndef SNAP_CONFINE_SANITY_H
#define SNAP_CONFINE_SANITY_H
#include <stdlib.h>
#include <stdio.h>

/**
 * Test context, contains global state for test execution.
 *
 * Currently this structure is only used to hold a FILE pointer to which all
 * test output is printed.
 **/
struct sc_test_context {
	FILE *stdtest;
};

/**
 * Test definition, contains bare essentials defining one test.
 *
 * This structure stores the name of the test, the execution flags as well as a
 * pointer to the test function.
 *
 * Test definition are created using the SC_TEST_REF macro.
 **/
struct sc_test_def {
	const char *fn_name;
	const unsigned flags;
	int (*check_fn) (const struct sc_test_def *, struct sc_test_context *);
};

/**
 * Flag indicating that a given test is expected to fail.
 **/
#define SC_XFAIL 1

/**
 * A singly-linked list of test definitions.
 *
 * This structure contains a simple list of test definitions.  It is maintained
 * with the help of the macro SC_LINK_TEST(), typically called from the
 * function defined by the macro SC_MODULE_TESTS().
 **/
struct sc_test_list {
	const struct sc_test_def *test_def;
	struct sc_test_list *next;
};

/**
 * Macro defining a prototype of a single test function.
 *
 * The test can have any name as long as the name is unique in the compilation
 * unit. The actual function name is sc_test__ ## _fn_name.
 **/
#define SC_TEST_FN(_fn_name) \
    static int \
    sc_test__ ## _fn_name(\
        const struct sc_test_def *test_def, \
        struct sc_test_context *test_ctx\
    )

/**
 * Macro defining a pointer to a static, constant test definition.
 *
 * The macro is typically used indirectly through the use of the macro
 * SC_LINK_TEST(). For the description of flags see struct sc_test_def.
 **/
#define SC_TEST_REF(_fn_name, _flags) \
    ({ \
     static const struct sc_test_def test_def = { \
         .fn_name = # _fn_name, \
         .flags = _flags, \
         .check_fn = sc_test__ ## _fn_name, \
         }; \
     &test_def; \
     })

/**
 * Macro appending a test definition to a list.
 *
 * This macro is typically used inside the function created by the
 * SC_MODULE_TESTS() macro. It appends a test definition node created by the
 * SC_TEST_REF() macro to the specified list. Note that the list pointer is
 * overwritten by this call.
 **/
#define SC_LINK_TEST(_list, _fn_name, _flags) \
        (_list) = ({ \
                   static struct sc_test_list _node; \
                   _node.test_def = SC_TEST_REF(_fn_name, _flags); \
                   _node.next = (_list); \
                   &_node; \
                   });

/**
 * Macro for defining a list of tests.
 **/
#define SC_MODULE_TESTS() \
    static void __attribute__((constructor)) sc_module_tests()

/**
 * Macro for printing diagnostic test messages.
 **/
#define SC_MSG(fmt, ...) \
    fprintf(test_ctx->stdtest, "(%s) " fmt, test_def->fn_name, ## __VA_ARGS__)

/**
 * Run a list of tests, returning the sum of the results.
 **/
int
sc_run_test_list(const struct sc_test_list *list, struct sc_test_context *ctx);

/**
 * The global list of all tests.
 **/
extern struct sc_test_list *sc_all_tests;

#endif				// SNAP_CONFINE_SANITY_H
