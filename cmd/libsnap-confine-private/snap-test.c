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

#include "snap.h"
#include "snap.c"

#include <glib.h>

static void test_sc_security_tag_validate(void)
{
	// First, test the names we know are good
	g_assert_true(sc_security_tag_validate("snap.name.app", "name", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.network-manager.NetworkManager",
		       "network-manager", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.f00.bar-baz1", "f00", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.foo.hook.bar", "foo", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.foo.hook.bar-baz", "foo", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.foo_instance.bar-baz", "foo_instance", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.foo_instance.hook.bar-baz", "foo_instance", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.foo_bar.hook.bar-baz", "foo_bar", NULL));

	// Now, test the names we know are bad
	g_assert_false(sc_security_tag_validate
		       ("pkg-foo.bar.0binary-bar+baz", "bar", NULL));
	g_assert_false(sc_security_tag_validate("pkg-foo_bar_1.1", NULL, NULL));
	g_assert_false(sc_security_tag_validate("appname/..", NULL, NULL));
	g_assert_false(sc_security_tag_validate("snap", NULL, NULL));
	g_assert_false(sc_security_tag_validate("snap.", NULL, NULL));
	g_assert_false(sc_security_tag_validate("snap.name", "name", NULL));
	g_assert_false(sc_security_tag_validate("snap.name.", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.app.", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.hook.", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap!name.app", "!name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.-name.app", "-name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name!app", "name!", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.-app", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.app!hook.foo", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.app.hook!foo", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.app.hook.-foo", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.app.hook.f00", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("sna.pname.app", "pname", NULL));
	g_assert_false(sc_security_tag_validate("snap.n@me.app", "n@me", NULL));
	g_assert_false(sc_security_tag_validate("SNAP.name.app", "name", NULL));
	g_assert_false(sc_security_tag_validate("snap.Name.app", "Name", NULL));
	// This used to be false but it's now allowed.
	g_assert_true(sc_security_tag_validate
		      ("snap.0name.app", "0name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.-name.app", "-name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.@app", "name", NULL));
	g_assert_false(sc_security_tag_validate(".name.app", "name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap..name.app", ".name", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name..app", "name.", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.name.app..", "name", NULL));
	// These contain invalid instance key
	g_assert_false(sc_security_tag_validate
		       ("snap.foo_.bar-baz", "foo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo_toolonginstance.bar-baz", "foo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo_inst@nace.bar-baz", "foo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo_in-stan-ce.bar-baz", "foo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo_in stan.bar-baz", "foo", NULL));

	// Test names that are both good, but snap name doesn't match security tag
	g_assert_false(sc_security_tag_validate
		       ("snap.foo.hook.bar", "fo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo.hook.bar", "fooo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo.hook.bar", "snap", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo.hook.bar", "bar", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo_instance.bar", "foo_bar", NULL));

	// Regression test 12to8
	g_assert_true(sc_security_tag_validate
		      ("snap.12to8.128to8", "12to8", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.123test.123test", "123test", NULL));
	g_assert_true(sc_security_tag_validate
		      ("snap.123test.hook.configure", "123test", NULL));

	// regression test snap.eon-edg-shb-pulseaudio.hook.connect-plug-i2c
	g_assert_true(sc_security_tag_validate
		      ("snap.foo.hook.connect-plug-i2c", "foo", NULL));

	// make sure that component hooks can be validated
	g_assert_true(sc_security_tag_validate
		      ("snap.foo+comp.hook.install", "foo", "comp"));
	g_assert_true(sc_security_tag_validate
		      ("snap.foo_instance+comp.hook.install", "foo_instance",
		       "comp"));
	// make sure that only hooks from components can be validated, not apps
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+comp.app", "foo", "comp"));

	// unexpected component names should not work
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+comp.hook.install", "foo", NULL));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+comp.hook.install", "foo", NULL));

	// missing component names when we expect one should not work
	g_assert_false(sc_security_tag_validate
		       ("snap.foo.hook.install", "foo", "comp"));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo.hook.install", "foo", "comp"));

	// mismatch component names should not work
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+comp.hook.install", "foo", "component"));

	// empty component name should not work
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+comp.hook.install", "foo", ""));

	// invalid component names should not work
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+coMp.hook.install", "foo", "coMp"));
	g_assert_false(sc_security_tag_validate
		       ("snap.foo+-omp.hook.install", "foo", "-omp"));

	// Security tag that's too long. The extra +2 is for the string
	// terminator and to allow us to make the tag too long to validate.
	char long_tag[SNAP_SECURITY_TAG_MAX_LEN + 2];
	memset(long_tag, 'b', sizeof long_tag);
	memcpy(long_tag, "snap.foo.b", sizeof "snap.foo.b" - 1);
	long_tag[sizeof long_tag - 1] = '\0';
	g_assert_true(strlen(long_tag) == SNAP_SECURITY_TAG_MAX_LEN + 1);
	g_assert_false(sc_security_tag_validate(long_tag, "foo", NULL));

	// If we make it one byte shorter it will be valid.
	long_tag[sizeof long_tag - 2] = '\0';
	g_assert_true(sc_security_tag_validate(long_tag, "foo", NULL));

}

static void test_sc_is_hook_security_tag(void)
{
	// First, test the names we know are good
	g_assert_true(sc_is_hook_security_tag("snap.foo.hook.bar"));
	g_assert_true(sc_is_hook_security_tag("snap.foo.hook.bar-baz"));
	g_assert_true(sc_is_hook_security_tag
		      ("snap.foo_instance.hook.bar-baz"));
	g_assert_true(sc_is_hook_security_tag("snap.foo_bar.hook.bar-baz"));
	g_assert_true(sc_is_hook_security_tag("snap.foo_bar.hook.f00"));
	g_assert_true(sc_is_hook_security_tag("snap.foo_bar.hook.f-0-0"));

	// Now, test the names we know are not valid hook security tags
	g_assert_false(sc_is_hook_security_tag("snap.foo_instance.bar-baz"));
	g_assert_false(sc_is_hook_security_tag("snap.name.app!hook.foo"));
	g_assert_false(sc_is_hook_security_tag("snap.name.app.hook!foo"));
	g_assert_false(sc_is_hook_security_tag("snap.name.app.hook.-foo"));
	g_assert_false(sc_is_hook_security_tag("snap.foo_bar.hook.0abcd"));
	g_assert_false(sc_is_hook_security_tag("snap.foo.hook.abc--"));
	g_assert_false(sc_is_hook_security_tag("snap.foo_bar.hook.!foo"));
	g_assert_false(sc_is_hook_security_tag("snap.foo_bar.hook.-foo"));
	g_assert_false(sc_is_hook_security_tag("snap.foo_bar.hook!foo"));
	g_assert_false(sc_is_hook_security_tag("snap.foo_bar.!foo"));
}

static void test_sc_snap_or_instance_name_validate(gconstpointer data)
{
	typedef void (*validate_func_t)(const char *, sc_error **);

	validate_func_t validate = (validate_func_t) data;
	bool is_instance =
	    (validate == sc_instance_name_validate) ? true : false;

	sc_error *err = NULL;

	// Smoke test, a valid snap name
	validate("hello-world", &err);
	g_assert_null(err);

	// Smoke test: invalid character 
	validate("hello world", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name must use lower case letters, digits or dashes");
	sc_error_free(err);

	// Smoke test: no letters
	validate("", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name must contain at least one letter");
	sc_error_free(err);

	// Smoke test: leading dash
	validate("-foo", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name cannot start with a dash");
	sc_error_free(err);

	// Smoke test: trailing dash
	validate("foo-", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name cannot end with a dash");
	sc_error_free(err);

	// Smoke test: double dash
	validate("f--oo", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name cannot contain two consecutive dashes");
	sc_error_free(err);

	// Smoke test: NULL name is not valid
	validate(NULL, &err);
	g_assert_nonnull(err);
	// the only case when instance name validation diverges from snap name
	// validation
	if (!is_instance) {
		g_assert_true(sc_error_match
			      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
		g_assert_cmpstr(sc_error_msg(err), ==,
				"snap name cannot be NULL");
	} else {
		g_assert_true(sc_error_match
			      (err, SC_SNAP_DOMAIN,
			       SC_SNAP_INVALID_INSTANCE_NAME));
		g_assert_cmpstr(sc_error_msg(err), ==,
				"snap instance name cannot be NULL");
	}
	sc_error_free(err);

	const char *valid_names[] = {
		"aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2"
	};
	for (size_t i = 0; i < sizeof valid_names / sizeof *valid_names; ++i) {
		g_test_message("checking valid snap name: %s", valid_names[i]);
		validate(valid_names[i], &err);
		g_assert_null(err);
	}
	const char *invalid_names[] = {
		// name cannot be empty
		"",
		// too short
		"a",
		// names cannot be too long
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"1111111111111111111111111111111111111111x",
		"x1111111111111111111111111111111111111111",
		"x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123", "1-2-3",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
	};
	for (size_t i = 0; i < sizeof invalid_names / sizeof *invalid_names;
	     ++i) {
		g_test_message("checking invalid snap name: >%s<",
			       invalid_names[i]);
		validate(invalid_names[i], &err);
		g_assert_nonnull(err);
		g_assert_true(sc_error_match
			      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
		sc_error_free(err);
	}
	// Regression test: 12to8 and 123test
	validate("12to8", &err);
	g_assert_null(err);
	validate("123test", &err);
	g_assert_null(err);

	// In case we switch to a regex, here's a test that could break things.
	const char good_bad_name[] = "u-94903713687486543234157734673284536758";
	char varname[sizeof good_bad_name] = { 0 };
	for (size_t i = 3; i <= sizeof varname - 1; i++) {
		g_assert_nonnull(memcpy(varname, good_bad_name, i));
		varname[i] = 0;
		g_test_message("checking valid snap name: >%s<", varname);
		validate(varname, &err);
		g_assert_null(err);
		sc_error_free(err);
	}
}

static void test_sc_snap_name_validate__respects_error_protocol(void)
{
	if (g_test_subprocess()) {
		sc_snap_name_validate("hello world", NULL);
		g_test_message("expected sc_snap_name_validate to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("snap name must use lower case letters, digits or dashes\n");
}

static void test_sc_instance_name_validate(void)
{
	sc_error *err = NULL;

	sc_instance_name_validate("hello-world", &err);
	g_assert_null(err);
	sc_instance_name_validate("hello-world_foo", &err);
	g_assert_null(err);

	// just the separator
	sc_instance_name_validate("_", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name must contain at least one letter");
	sc_error_free(err);

	// just name, with separator, missing instance key
	sc_instance_name_validate("hello-world_", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_KEY));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"instance key must contain at least one letter or digit");
	sc_error_free(err);

	// only separator and instance key, missing name
	sc_instance_name_validate("_bar", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name must contain at least one letter");
	sc_error_free(err);

	sc_instance_name_validate("", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap name must contain at least one letter");
	sc_error_free(err);

	// third separator
	sc_instance_name_validate("foo_bar_baz", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap instance name can contain only one underscore");
	sc_error_free(err);

	// too long, 52
	sc_instance_name_validate
	    ("0123456789012345678901234567890123456789012345678901", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"snap instance name can be at most 51 characters long");
	sc_error_free(err);

	const char *valid_names[] = {
		"aa", "aaa", "aaaa",
		"aa_a", "aa_1", "aa_123", "aa_0123456789",
	};
	for (size_t i = 0; i < sizeof valid_names / sizeof *valid_names; ++i) {
		g_test_message("checking valid instance name: %s",
			       valid_names[i]);
		sc_instance_name_validate(valid_names[i], &err);
		g_assert_null(err);
	}
	const char *invalid_names[] = {
		// too short
		"a",
		// only letters and digits in the instance key
		"a_--23))", "a_ ", "a_091234#", "a_123_456",
		// up to 10 characters for the instance key
		"a_01234567891", "a_0123456789123",
		// snap name must not be more than 40 characters, regardless of instance
		// key
		"01234567890123456789012345678901234567890_foobar",
		"01234567890123456789-01234567890123456789_foobar",
		// instance key  must be plain ASCII
		"foobar_日本語",
		// way too many underscores
		"foobar_baz_zed_daz",
		"foobar______",
	};
	for (size_t i = 0; i < sizeof invalid_names / sizeof *invalid_names;
	     ++i) {
		g_test_message("checking invalid instance name: >%s<",
			       invalid_names[i]);
		sc_instance_name_validate(invalid_names[i], &err);
		g_assert_nonnull(err);
		sc_error_free(err);
	}
}

static void test_sc_snap_drop_instance_key_no_dest(void)
{
	if (g_test_subprocess()) {
		sc_snap_drop_instance_key("foo_bar", NULL, 0);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();

}

static void test_sc_snap_drop_instance_key_short_dest(void)
{
	if (g_test_subprocess()) {
		char dest[10] = { 0 };
		sc_snap_drop_instance_key("foo-foo-foo-foo-foo_bar", dest,
					  sizeof dest);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

static void test_sc_snap_drop_instance_key_short_dest2(void)
{
	if (g_test_subprocess()) {
		char dest[3] = { 0 };	// "foo" sans the nil byte
		sc_snap_drop_instance_key("foo", dest, sizeof dest);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

static void test_sc_snap_drop_instance_key_no_name(void)
{
	if (g_test_subprocess()) {
		char dest[10] = { 0 };
		sc_snap_drop_instance_key(NULL, dest, sizeof dest);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

static void test_sc_snap_drop_instance_key_short_dest_max(void)
{
	if (g_test_subprocess()) {
		char dest[SNAP_NAME_LEN + 1] = { 0 };
		/* 40 chars (max valid length), pretend dest is the same length, no space for terminator */
		sc_snap_drop_instance_key
		    ("01234567890123456789012345678901234567890", dest,
		     sizeof dest - 1);
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

static void test_sc_snap_drop_instance_key_basic(void)
{
	char name[SNAP_NAME_LEN + 1] = { 0xff };

	sc_snap_drop_instance_key("foo_bar", name, sizeof name);
	g_assert_cmpstr(name, ==, "foo");

	memset(name, 0xff, sizeof name);
	sc_snap_drop_instance_key("foo-bar_bar", name, sizeof name);
	g_assert_cmpstr(name, ==, "foo-bar");

	memset(name, 0xff, sizeof name);
	sc_snap_drop_instance_key("foo-bar", name, sizeof name);
	g_assert_cmpstr(name, ==, "foo-bar");

	memset(name, 0xff, sizeof name);
	sc_snap_drop_instance_key("_baz", name, sizeof name);
	g_assert_cmpstr(name, ==, "");

	memset(name, 0xff, sizeof name);
	sc_snap_drop_instance_key("foo", name, sizeof name);
	g_assert_cmpstr(name, ==, "foo");

	memset(name, 0xff, sizeof name);
	/* 40 chars - snap name length */
	sc_snap_drop_instance_key("0123456789012345678901234567890123456789",
				  name, sizeof name);
	g_assert_cmpstr(name, ==, "0123456789012345678901234567890123456789");
}

static void test_sc_snap_split_instance_name_basic(void)
{
	char name[SNAP_NAME_LEN + 1] = { 0xff };
	char instance[20] = { 0xff };

	sc_snap_split_instance_name("foo_bar", name, sizeof name, instance,
				    sizeof instance);
	g_assert_cmpstr(name, ==, "foo");
	g_assert_cmpstr(instance, ==, "bar");
}

static void test_sc_snap_split_snap_component_basic(void)
{
	char snap_name[SNAP_NAME_LEN + 1] = { 0xff };
	char component_name[SNAP_NAME_LEN + 1] = { 0xff };

	sc_snap_split_snap_component("foo+bar", snap_name, sizeof snap_name,
				     component_name, sizeof component_name);
	g_assert_cmpstr(snap_name, ==, "foo");
	g_assert_cmpstr(component_name, ==, "bar");
}

static void test_sc_snap_component_validate(void)
{
	sc_error *err = NULL;
	sc_snap_component_validate("snapname+compname", NULL, &err);
	g_assert_null(err);

	sc_snap_component_validate("snap-name+comp-name", NULL, &err);
	g_assert_null(err);

	// check that we fail if the snap name isn't in the snap component
	sc_snap_component_validate("snapname+compname", "othername", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT));
	sc_snap_component_validate("snapname+compname", "othername_instance",
				   &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT));

	// component name should never have an instance key in it, so this should
	// fail
	sc_snap_component_validate("snapname_instance+compname",
				   "snapname_instance", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT));
	sc_snap_component_validate("snapname_instance+compname", "snapname",
				   &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT));

	// check that we can validate the snap name in the snap component
	sc_snap_component_validate("snapname+compname", "snapname", &err);
	g_assert_null(err);
	sc_snap_component_validate("snapname+compname", "snapname_instance",
				   &err);
	g_assert_null(err);

	const char *cases[] = {
		NULL, "snap-name+", "+comp-name", "snap-name",
		"snap-name+comp_name",
		"loooooooooooooooooooooooooooong-snap-name+comp-name",
		"snap-name+loooooooooooooooooooooooooooong-comp-name",
	};

	for (size_t i = 0; i < sizeof cases / sizeof *cases; ++i) {
		g_test_message("checking invalid snap name: %s", cases[i]);
		sc_snap_component_validate(cases[i], NULL, &err);
		g_assert_nonnull(err);
		g_assert_true(sc_error_match
			      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT));
	}
}

static void test_sc_snap_component_validate_respects_error_protocol(void)
{
	if (g_test_subprocess()) {
		sc_snap_component_validate("hello world+comp name", NULL, NULL);
		g_test_message("expected sc_snap_name_validate to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("snap name in component must use lower case letters, digits or dashes\n");
}

static void __attribute__((constructor)) init(void)
{
	g_test_add_func("/snap/sc_security_tag_validate",
			test_sc_security_tag_validate);
	g_test_add_func("/snap/sc_is_hook_security_tag",
			test_sc_is_hook_security_tag);

	g_test_add_data_func("/snap/sc_snap_name_validate",
			     sc_snap_name_validate,
			     test_sc_snap_or_instance_name_validate);
	g_test_add_func("/snap/sc_snap_name_validate/respects_error_protocol",
			test_sc_snap_name_validate__respects_error_protocol);

	g_test_add_data_func("/snap/sc_instance_name_validate/just_name",
			     sc_instance_name_validate,
			     test_sc_snap_or_instance_name_validate);
	g_test_add_func("/snap/sc_instance_name_validate/full",
			test_sc_instance_name_validate);

	g_test_add_func("/snap/sc_snap_drop_instance_key/basic",
			test_sc_snap_drop_instance_key_basic);
	g_test_add_func("/snap/sc_snap_drop_instance_key/no_dest",
			test_sc_snap_drop_instance_key_no_dest);
	g_test_add_func("/snap/sc_snap_drop_instance_key/no_name",
			test_sc_snap_drop_instance_key_no_name);
	g_test_add_func("/snap/sc_snap_drop_instance_key/short_dest",
			test_sc_snap_drop_instance_key_short_dest);
	g_test_add_func("/snap/sc_snap_drop_instance_key/short_dest2",
			test_sc_snap_drop_instance_key_short_dest2);
	g_test_add_func("/snap/sc_snap_drop_instance_key/short_dest_max",
			test_sc_snap_drop_instance_key_short_dest_max);

	g_test_add_func("/snap/sc_snap_split_instance_name/basic",
			test_sc_snap_split_instance_name_basic);
	g_test_add_func("/snap/sc_snap_split_snap_component/basic",
			test_sc_snap_split_snap_component_basic);
	g_test_add_func("/snap/sc_snap_component_validate",
			test_sc_snap_component_validate);
	g_test_add_func
	    ("/snap/sc_snap_component_validate/respects_error_protocol",
	     test_sc_snap_component_validate_respects_error_protocol);
}
