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

#include "mount-entry-change.h"
#include "mount-entry-change.c"

#include "test-utils.h"
#include "test-data.h"

#include <glib.h>

// Scenario: there is nothing to do yet at all.
static void test_sc_compute_required_mount_changes__scenario0()
{
	struct sc_mount_entry *current = NULL;
	struct sc_mount_entry *desired = NULL;
	struct sc_mount_change *change;

	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify) sc_mount_change_free_chain,
			     change);

	g_assert_null(change);

}

// Scenario: the current profile contains things but the desired profile does
// not. We should see two unmounts taking place.
static void test_sc_compute_required_mount_changes__scenario1()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change *change;

	sc_test_write_lines("current.fstab",
			    test_entry_str_1, test_entry_str_2, NULL);
	sc_test_write_lines("desired.fstab", NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify) sc_mount_change_free_chain,
			     change);

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_1(change->entry);

	change = change->next;

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_2(change->entry);

	change = change->next;

	g_assert_null(change);
}

// Scenario: the current profile is empty but the desired profile
// contains two entries. We should see two mounts taking place.
static void test_sc_compute_required_mount_changes__scenario2()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change *change;

	sc_test_write_lines("current.fstab", NULL);
	sc_test_write_lines("desired.fstab",
			    test_entry_str_1, test_entry_str_2, NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify) sc_mount_change_free_chain,
			     change);

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_1(change->entry);

	change = change->next;

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_2(change->entry);

	change = change->next;

	g_assert_null(change);
}

// Scenario: the current profile contains one entry but the desired profile
// contains two entries. We should see one mount change (for the 2nd entry).
static void test_sc_compute_required_mount_changes__scenario3()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change *change;

	sc_test_write_lines("current.fstab", test_entry_str_1, NULL);
	sc_test_write_lines("desired.fstab",
			    test_entry_str_1, test_entry_str_2, NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify) sc_mount_change_free_chain,
			     change);

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_2(change->entry);

	change = change->next;

	g_assert_null(change);
}

// Scenario: the current profile contains one entry and the desired profile
// contains one entry but they are different. We should see the unmount
// followed by the mount.
static void test_sc_compute_required_mount_changes__scenario4()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change *change;

	sc_test_write_lines("current.fstab", test_entry_str_1, NULL);
	sc_test_write_lines("desired.fstab", test_entry_str_2, NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify) sc_mount_change_free_chain,
			     change);

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_1(change->entry);

	change = change->next;

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change->entry);
	test_looks_like_test_entry_2(change->entry);

	change = change->next;

	g_assert_null(change);
}

// Scenario: desired A, B current B, C behaves correctly (B is untouched).
static void test_sc_compute_required_mount_changes__scenario5()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change *change;

	sc_test_write_lines("desired.fstab", "A A A A 0 0", "B B B B 0 0",
			    NULL);
	sc_test_write_lines("current.fstab", "B B B B 0 0", "C C C C 0 0",
			    NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify) sc_mount_change_free_chain,
			     change);

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change->entry);
	g_assert_cmpstr(change->entry->entry.mnt_fsname, ==, "C");

	change = change->next;

	g_assert_nonnull(change);
	g_assert_cmpint(change->action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change->entry);
	g_assert_cmpstr(change->entry->entry.mnt_fsname, ==, "A");

	change = change->next;

	g_assert_null(change);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/0",
	     test_sc_compute_required_mount_changes__scenario0);
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/1",
	     test_sc_compute_required_mount_changes__scenario1);
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/2",
	     test_sc_compute_required_mount_changes__scenario2);
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/3",
	     test_sc_compute_required_mount_changes__scenario3);
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/4",
	     test_sc_compute_required_mount_changes__scenario4);
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/5",
	     test_sc_compute_required_mount_changes__scenario5);
}
