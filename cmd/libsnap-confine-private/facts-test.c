/*
 * Copyright (C) 2018 Canonical Ltd
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

#include "facts.h"
#include "facts.c"

#include <sys/stat.h>
#include <unistd.h>

#include <glib.h>

static void remove_file(gpointer * fname)
{
	chmod((const char *)fname, 0644);
	unlink((const char *)fname);
}

static void test_sc_load_facts(void)
{
	const char *fname = "facts.test";
	char *facts = NULL;

	g_test_queue_destroy((GDestroyNotify) remove_file, (gpointer) fname);

	/* The facts file can be missing. */
	unlink(fname);
	facts = sc_load_facts(fname);
	g_test_queue_free((gpointer) facts);
	g_assert_cmpstr(facts, ==, NULL);

	/* The facts file can be empty. */
	g_file_set_contents(fname, "", -1, NULL);
	facts = sc_load_facts(fname);
	g_test_queue_free((gpointer) facts);
	g_assert_cmpstr(facts, ==, "");

	/* The facts file can have reasonable contents. */
	g_file_set_contents(fname, "key=value\nfoo=bar\n", -1, NULL);
	facts = sc_load_facts(fname);
	g_test_queue_free((gpointer) facts);
	g_assert_cmpstr(facts, ==, "key=value\nfoo=bar\n");
}

static void test_sc_load_facts__too_big(void)
{
	const char *fname = "facts.test";

	g_test_queue_destroy((GDestroyNotify) remove_file, (gpointer) fname);

	if (g_test_subprocess()) {
		/* The facts file cannot be larger than 16KB */
		char buf[16 * 1024];
		memset(buf, 'x', sizeof buf);
		g_file_set_contents(fname, buf, -1, NULL);
		(void)sc_load_facts(fname);
		g_test_message("expected sc_load_facts not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("cannot load facts larger than 16KB\n");
}

static void test_sc_load_facts__cannot_open(void)
{
	const char *fname = "facts.test";

	g_test_queue_destroy((GDestroyNotify) remove_file, (gpointer) fname);

	if (g_test_subprocess()) {
		g_file_set_contents(fname, "key=value\nfoo=bar\n", -1, NULL);
		chmod(fname, 0000);
		(void)sc_load_facts(fname);
		g_test_message("expected sc_load_facts not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("cannot open facts file facts.test: Permission denied\n");
}

static void test_sc_query_fact(void)
{
	const char *facts = "f1=1\nf2=22\nf3=333\n";

	/* Searching in and for various NULL or empty things. */
	g_assert_cmpuint(sc_query_fact(NULL, NULL, NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("name=value", NULL, NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact(NULL, "name", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact(NULL, "", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("", NULL, NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("", "", NULL, 0), ==, 0);

	/* Querying for value size. */
	g_assert_cmpuint(sc_query_fact("name=value\n", "name", NULL, 0), ==, 6);
	g_assert_cmpuint(sc_query_fact("name=value", "name", NULL, 0), ==, 6);
	g_assert_cmpuint(sc_query_fact("name=\n", "name", NULL, 0), ==, 1);
	g_assert_cmpuint(sc_query_fact("name=", "name", NULL, 0), ==, 1);
	g_assert_cmpuint(sc_query_fact("\n", "name", NULL, 0), ==, 0);

	g_assert_cmpuint(sc_query_fact(facts, "f1", NULL, 0), ==, 1 + 1);
	g_assert_cmpuint(sc_query_fact(facts, "f2", NULL, 0), ==, 2 + 1);
	g_assert_cmpuint(sc_query_fact(facts, "f3", NULL, 0), ==, 3 + 1);

	/* Searching without success */
	g_assert_cmpuint(sc_query_fact("name", "nam", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("name=", "nam", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("namevalue=", "nam", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("name", "name=", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("name=", "name=", NULL, 0), ==, 0);
	g_assert_cmpuint(sc_query_fact("namevalue=", "name=", NULL, 0), ==, 0);

	char buf1[1], buf2[2];
	size_t n;

	/* The value is "1" but we have 0 bytes! */
	memset(buf1, 0777, sizeof buf1);
	n = sc_query_fact(facts, "f1", buf1, 0);
	g_assert_cmpuint(n, ==, 1 + 1);

	/* The value is "1" but we have space for just "". */
	memset(buf1, 0777, sizeof buf1);
	n = sc_query_fact(facts, "f1", buf1, sizeof buf1);
	g_assert_cmpuint(n, ==, 1 + 1);
	g_assert_cmpstr(buf1, ==, "");

	/* The value is "22" but we have space for just "2". */
	memset(buf2, 0777, sizeof buf2);
	n = sc_query_fact(facts, "f2", buf2, sizeof buf2);
	g_assert_cmpuint(n, ==, 2 + 1);
	g_assert_cmpstr(buf2, ==, "2");

	char buf[16];

	/* Retrieval of values */
	memset(buf, 0777, sizeof buf);
	n = sc_query_fact(facts, "f1", buf, sizeof buf);
	g_assert_cmpuint(n, ==, 1 + 1);
	g_assert_cmpstr(buf, ==, "1");

	memset(buf, 0777, sizeof buf);
	n = sc_query_fact(facts, "f2", buf, sizeof buf);
	g_assert_cmpuint(n, ==, 2 + 1);
	g_assert_cmpstr(buf, ==, "22");

	memset(buf, 0777, sizeof buf);
	n = sc_query_fact(facts, "f3", buf, sizeof buf);
	g_assert_cmpuint(n, ==, 3 + 1);
	g_assert_cmpstr(buf, ==, "333");
}

static void test_sc_get_bool_fact(void)
{
	g_assert_false(sc_get_bool_fact("layouts=banana", "layouts", false));
	g_assert_false(sc_get_bool_fact("layouts=", "layouts", false));
	g_assert_false(sc_get_bool_fact("layouts=false", "layouts", false));
	g_assert_true(sc_get_bool_fact("layouts=true", "layouts", true));
	g_assert_true(sc_get_bool_fact("hotplug=true", "layouts", true));
}

static void __attribute__ ((constructor)) init(void)
{
	g_test_add_func("/facts/load", test_sc_load_facts);
	g_test_add_func("/facts/load::2big", test_sc_load_facts__too_big);
	g_test_add_func("/facts/load::cannot-open",
			test_sc_load_facts__cannot_open);
	g_test_add_func("/facts/query", test_sc_query_fact);
	g_test_add_func("/facts/get_bool", test_sc_get_bool_fact);
}
