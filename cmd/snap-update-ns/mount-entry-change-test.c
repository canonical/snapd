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

#include "../libsnap-confine-private/string-utils.h"

#include <stdarg.h>

#include <glib.h>

static void test_sc_mount_action_to_str()
{
	g_assert_cmpstr(sc_mount_action_to_str(SC_ACTION_NONE), ==, "none");
	g_assert_cmpstr(sc_mount_action_to_str(SC_ACTION_MOUNT), ==, "mount");
	g_assert_cmpstr(sc_mount_action_to_str(SC_ACTION_UNMOUNT), ==,
			"unmount");
	g_assert_cmpstr(sc_mount_action_to_str(-1), ==, "???");
}

static void g_assert_mount_entry_eq(const struct sc_mount_entry *entry1,
				    const struct sc_mount_entry *entry2)
{
	g_assert_cmpstr(entry1->entry.mnt_fsname, ==, entry2->entry.mnt_fsname);
	g_assert_cmpstr(entry1->entry.mnt_dir, ==, entry2->entry.mnt_dir);
	g_assert_cmpstr(entry1->entry.mnt_type, ==, entry2->entry.mnt_type);
	g_assert_cmpstr(entry1->entry.mnt_opts, ==, entry2->entry.mnt_opts);
	g_assert_cmpint(entry1->entry.mnt_freq, ==, entry2->entry.mnt_freq);
	g_assert_cmpint(entry1->entry.mnt_passno, ==, entry2->entry.mnt_passno);
}

static void sc_mount_entry_to_str(char *buf, size_t buf_size,
				  const struct sc_mount_entry *entry)
{
	// NOTE: this is naive that it doesn't escape spaces and some other special
	// characters but we only use it for testing / comparison with test data.
	sc_must_snprintf(buf, buf_size, "%s %s %s %s %d %d",
			 entry->entry.mnt_fsname, entry->entry.mnt_dir,
			 entry->entry.mnt_type, entry->entry.mnt_opts,
			 entry->entry.mnt_freq, entry->entry.mnt_passno);
}

__attribute__ ((sentinel))
static void test_assert_change_list(const struct sc_mount_change *change, ...);

static void test_assert_change_list(const struct sc_mount_change *change, ...)
{
	va_list ap;

	const struct sc_mount_entry *entry;
	enum sc_mount_action action;
	char actual_buf[1000], expected_buf[1000];

	va_start(ap, change);
	while ((entry = va_arg(ap, struct sc_mount_entry *)) != NULL) {
		action = va_arg(ap, enum sc_mount_action);

		g_assert_nonnull(change);
		if (change == NULL) {
			g_test_message("actual change is NULL");
			break;	// break in case data and test disagree
		}

		sc_mount_entry_to_str(actual_buf, sizeof actual_buf,
				      change->entry);
		g_test_message("actual change %s: %s",
			       sc_mount_action_to_str(change->action),
			       actual_buf);

		sc_mount_entry_to_str(expected_buf, sizeof expected_buf, entry);
		g_test_message("expected change %s: %s",
			       sc_mount_action_to_str(action), expected_buf);

		g_assert_mount_entry_eq(change->entry, entry);
		if (sc_compare_mount_entry(change->entry, entry)
		    != 0) {
			break;
		}
		g_assert_cmpint(change->action, ==, action);
		if (change->action != action) {
			break;
		}

		change = change->next;
	}
	g_assert_null(change);
	va_end(ap);
}

// Scenario: there is nothing to do yet at all.
static void test_sc_compute_required_mount_changes__scenario0()
{
	// Both current and desired don't exist.
	struct sc_mount_entry_list *current;
	current = sc_load_mount_profile("current.fstab");
	g_assert_null(current->first);
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	desired = sc_load_mount_profile("desired.fstab");
	g_assert_null(desired->first);
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	test_assert_change_list(change, NULL);
}

// Scenario: the current profile contains things but the desired profile does
// not. We should see two unmounts taking place.
static void test_sc_compute_required_mount_changes__scenario1()
{
	struct sc_mount_entry_list *current;
	sc_test_write_lines("current.fstab",
			    test_entry_str_1, test_entry_str_2, NULL);
	current = sc_load_mount_profile("current.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	sc_test_write_lines("desired.fstab", NULL);
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	test_assert_change_list(change,
				// Unmount 2nd entry
				&test_entry_2, SC_ACTION_UNMOUNT,
				// Unmount 1st entry
				&test_entry_1, SC_ACTION_UNMOUNT, NULL);
}

// Scenario: the current profile is empty but the desired profile
// contains two entries. We should see two mounts taking place.
static void test_sc_compute_required_mount_changes__scenario2()
{
	struct sc_mount_entry_list *current;
	sc_test_write_lines("current.fstab", NULL);
	current = sc_load_mount_profile("current.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	sc_test_write_lines("desired.fstab",
			    test_entry_str_1, test_entry_str_2, NULL);
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	test_assert_change_list(change,
				// mount 1st entry
				&test_entry_1, SC_ACTION_MOUNT,
				// mount 2nd entry
				&test_entry_2, SC_ACTION_MOUNT, NULL);
}

// Scenario: the current profile contains one entry but the desired profile
// contains two entries. We should see one mount change (for the 2nd entry).
static void test_sc_compute_required_mount_changes__scenario3()
{
	struct sc_mount_entry_list *current;
	sc_test_write_lines("current.fstab", test_entry_str_1, NULL);
	current = sc_load_mount_profile("current.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	sc_test_write_lines("desired.fstab",
			    test_entry_str_1, test_entry_str_2, NULL);
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	test_assert_change_list(change,
				// mount 2nd entry
				&test_entry_2, SC_ACTION_MOUNT, NULL);
}

// Scenario: the current profile contains one entry and the desired profile
// contains one entry but they are different. We should see the unmount
// followed by the mount.
static void test_sc_compute_required_mount_changes__scenario4()
{
	struct sc_mount_entry_list *current;
	sc_test_write_lines("current.fstab", test_entry_str_1, NULL);
	current = sc_load_mount_profile("current.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	sc_test_write_lines("desired.fstab", test_entry_str_2, NULL);
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	test_assert_change_list(change, &test_entry_1,
				SC_ACTION_UNMOUNT,
				&test_entry_2, SC_ACTION_MOUNT, NULL);
}

// Scenario: desired A, B current B, C behaves correctly (B is untouched).
static void test_sc_compute_required_mount_changes__scenario5()
{
	struct sc_mount_entry_list *current;
	sc_test_write_lines("current.fstab",
			    "B B B B 0 0", "C C C C 0 0", NULL);
	current = sc_load_mount_profile("current.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	sc_test_write_lines("desired.fstab",
			    "A A A A 0 0", "B B B B 0 0", NULL);
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	const struct sc_mount_entry C = {
		.entry = {
			  "C", "C", "C", "C"}
	};
	const struct sc_mount_entry A = {
		.entry = {
			  "A", "A", "A", "A"}
	};
	test_assert_change_list(change,
				&C, SC_ACTION_UNMOUNT,
				&A, SC_ACTION_MOUNT, NULL);
}

// Scenario: desired A, A/B, current: A A/B with the tweak that A changes
// subtly (e.g. different type of mount vs what we had earlier).
static void test_sc_compute_required_mount_changes__scenario6()
{
	struct sc_mount_entry_list *current;
	sc_test_write_lines("current.fstab",
			    "/dev/sda1 /foo ext4 rw 0 0",
			    "/dev/loop7 /foo/bar squashfs ro 0 0", NULL);
	current = sc_load_mount_profile("current.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, current);

	struct sc_mount_entry_list *desired;
	sc_test_write_lines("desired.fstab",
			    "/dev/sda2 /foo ext4 rw 0 0",
			    "/dev/loop7 /foo/bar squashfs ro 0 0", NULL);
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify)
			     sc_free_mount_entry_list, desired);

	struct sc_mount_change *change;
	change = sc_compute_required_mount_changes(desired, current);
	g_test_queue_destroy((GDestroyNotify)
			     sc_mount_change_free_chain, change);
	const struct sc_mount_entry parent_current = {
		.entry = {
			  "/dev/sda1", "/foo", "ext4", "rw"}
	};
	const struct sc_mount_entry parent_desired = {
		.entry = {
			  "/dev/sda2", "/foo", "ext4", "rw"}
	};
	const struct sc_mount_entry child = {
		.entry = {
			  "/dev/loop7", "/foo/bar", "squashfs", "ro"}
	};
	test_assert_change_list(change,
				// Unmount the child and then the parent.
				&child, SC_ACTION_UNMOUNT,
				&parent_current, SC_ACTION_UNMOUNT,
				// Mount the new parent and then the child.
				&parent_desired, SC_ACTION_MOUNT,
				&child, SC_ACTION_MOUNT, NULL);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mount-entry-change/sc_mount_action_to_str",
			test_sc_mount_action_to_str);
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
	g_test_add_func
	    ("/mount-entry-change/sc_compute_required_mount_changes/6",
	     test_sc_compute_required_mount_changes__scenario6);
}
