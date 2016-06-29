/*
 * Copyright (C) 2015 Canonical Ltd
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
#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "sanity.h"
#include <unistd.h>
#include <sys/types.h>
#include <sys/wait.h>

struct sc_test_list *sc_all_tests = NULL;

/** Run a single test. */
static int
sc_run_test(const struct sc_test_def *test_def,
	    struct sc_test_context *test_ctx)
{
	fprintf(test_ctx->stdtest, "(%s) BEGIN\n", test_def->fn_name);

	pid_t pid = fork();
	if (pid == -1) {
		fprintf(test_ctx->stdtest, "cannot fork, aborting\n");
		abort();
	}
	if (pid == 0) {
		int result = test_def->check_fn(test_def, test_ctx);
		exit(result);
	}
	int status = 0;
	int result;
	if (waitpid(pid, &status, 0) != pid) {
		perror("waitpid failed");
		abort();
	}

	if (WIFEXITED(status)) {
		result = WEXITSTATUS(status);
	} else {
		result = 1;
	}
	if (test_def->flags & SC_XFAIL) {
		result = !result;
	}
	if (result == 0) {
		fprintf(test_ctx->stdtest, "(%s) PASS\n", test_def->fn_name);
	} else {
		fprintf(test_ctx->stdtest, "(%s) FAIL\n", test_def->fn_name);
	}
	return result;
}

SC_TEST_FN(pass)
{
	SC_MSG("Test that returns zero should PASS\n");
	return 0;
}

SC_TEST_FN(fail)
{
	SC_MSG("Test that returns non-zero should FAIL\n");
	return 1;
}

SC_TEST_FN(abort)
{
	SC_MSG("Test that exits abnormally should FAIL\n");
	abort();
	return 0;
}

SC_MODULE_TESTS()
{
	SC_LINK_TEST(sc_all_tests, pass, 0);
	SC_LINK_TEST(sc_all_tests, fail, SC_XFAIL);
	SC_LINK_TEST(sc_all_tests, abort, SC_XFAIL);
}

int
sc_run_test_list(const struct sc_test_list *list, struct sc_test_context *ctx)
{
	int result = 0;
	for (; list != NULL; list = list->next) {
		result += sc_run_test(list->test_def, ctx);
	}
	return result;
}
