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

#include "mount-entry.h"
#include "mount-entry.c"

#include <stdarg.h>

#include <glib.h>

static const char *test_entry_str_1 = "fsname-1 dir-1 type-1 opts-1 1 2";
static const char *test_entry_str_2 = "fsname-2 dir-2 type-2 opts-2 3 4";

static const struct sc_mount_entry test_entry_1 = {
	.mnt_fsname = "fsname-1",
	.mnt_dir = "dir-1",
	.mnt_type = "type-1",
	.mnt_opts = "opts-1",
	.mnt_freq = 1,
	.mnt_passno = 2,
};

static void test_looks_like_test_entry_1(const struct sc_mount_entry *entry)
{
	g_assert_cmpstr(entry->mnt_fsname, ==, "fsname-1");
	g_assert_cmpstr(entry->mnt_dir, ==, "dir-1");
	g_assert_cmpstr(entry->mnt_type, ==, "type-1");
	g_assert_cmpstr(entry->mnt_opts, ==, "opts-1");
	g_assert_cmpint(entry->mnt_freq, ==, 1);
	g_assert_cmpint(entry->mnt_passno, ==, 2);
}

static const struct sc_mount_entry test_entry_2 = {
	.mnt_fsname = "fsname-2",
	.mnt_dir = "dir-2",
	.mnt_type = "type-2",
	.mnt_opts = "opts-2",
	.mnt_freq = 3,
	.mnt_passno = 4,
};

static void test_looks_like_test_entry_2(const struct sc_mount_entry *entry)
{
	g_assert_cmpstr(entry->mnt_fsname, ==, "fsname-2");
	g_assert_cmpstr(entry->mnt_dir, ==, "dir-2");
	g_assert_cmpstr(entry->mnt_type, ==, "type-2");
	g_assert_cmpstr(entry->mnt_opts, ==, "opts-2");
	g_assert_cmpint(entry->mnt_freq, ==, 3);
	g_assert_cmpint(entry->mnt_passno, ==, 4);
}

static const struct mntent test_mnt_1 = {
	.mnt_fsname = "fsname-1",
	.mnt_dir = "dir-1",
	.mnt_type = "type-1",
	.mnt_opts = "opts-1",
	.mnt_freq = 1,
	.mnt_passno = 2,
};

static const struct mntent test_mnt_2 = {
	.mnt_fsname = "fsname-2",
	.mnt_dir = "dir-2",
	.mnt_type = "type-2",
	.mnt_opts = "opts-2",
	.mnt_freq = 3,
	.mnt_passno = 4,
};

static void test_write_lines(const char *name, ...) __attribute__ ((sentinel));

static void test_write_lines(const char *name, ...)
{
	FILE *f = NULL;
	f = fopen(name, "wt");
	va_list ap;
	va_start(ap, name);
	const char *line;
	while ((line = va_arg(ap, const char *)) != NULL) {
		fprintf(f, "%s\n", line);
	}
	va_end(ap);
	fclose(f);
}

static void test_sc_load_mount_profile()
{
	struct sc_mount_entry *fstab
	    __attribute__ ((cleanup(sc_cleanup_mount_entry_list))) = NULL;
	struct sc_mount_entry *entry;
	test_write_lines("test.fstab", test_entry_str_1, test_entry_str_2,
			 NULL);
	fstab = sc_load_mount_profile("test.fstab");
	g_assert_nonnull(fstab);

	entry = fstab;
	test_looks_like_test_entry_1(entry);
	g_assert_nonnull(entry->next);

	entry = entry->next;
	test_looks_like_test_entry_2(entry);
	g_assert_null(entry->next);
}

static void test_sc_save_mount_profile()
{
	struct sc_mount_entry entry_1 = test_entry_1;
	struct sc_mount_entry entry_2 = test_entry_2;
	entry_1.next = &entry_2;
	entry_2.next = NULL;

	// We can save the profile defined above.
	sc_save_mount_profile(&entry_1, "test.fstab");

	// After reading the generated file it looks as expected.
	FILE *f = fopen("test.fstab", "rt");
	g_assert_nonnull(f);
	char *line = NULL;
	size_t n = 0;
	ssize_t num_read;

	num_read = getline(&line, &n, f);
	g_assert_cmpint(num_read, >, -0);
	g_assert_cmpstr(line, ==, "fsname-1 dir-1 type-1 opts-1 1 2\n");

	num_read = getline(&line, &n, f);
	g_assert_cmpint(num_read, >, -0);
	g_assert_cmpstr(line, ==, "fsname-2 dir-2 type-2 opts-2 3 4\n");

	num_read = getline(&line, &n, f);
	g_assert_cmpint(num_read, ==, -1);

	free(line);
	fclose(f);
}

static void test_sc_compare_mount_entry()
{
	// Do trivial comparison checks.
	g_assert_cmpint(sc_compare_mount_entry(&test_entry_1, &test_entry_1),
			==, 0);
	g_assert_cmpint(sc_compare_mount_entry(&test_entry_1, &test_entry_2), <,
			0);
	g_assert_cmpint(sc_compare_mount_entry(&test_entry_2, &test_entry_1), >,
			0);
	g_assert_cmpint(sc_compare_mount_entry(&test_entry_2, &test_entry_2),
			==, 0);

	// Ensure that each field is compared.
	struct sc_mount_entry a = test_entry_1;
	struct sc_mount_entry b = test_entry_1;
	g_assert_cmpint(sc_compare_mount_entry(&a, &b), ==, 0);

	b.mnt_fsname = test_entry_2.mnt_fsname;
	g_assert_cmpint(sc_compare_mount_entry(&a, &b), <, 0);
	b = test_entry_1;

	b.mnt_dir = test_entry_2.mnt_dir;
	g_assert_cmpint(sc_compare_mount_entry(&a, &b), <, 0);
	b = test_entry_1;

	b.mnt_opts = test_entry_2.mnt_opts;
	g_assert_cmpint(sc_compare_mount_entry(&a, &b), <, 0);
	b = test_entry_1;

	b.mnt_freq = test_entry_2.mnt_freq;
	g_assert_cmpint(sc_compare_mount_entry(&a, &b), <, 0);
	b = test_entry_1;

	b.mnt_passno = test_entry_2.mnt_passno;
	g_assert_cmpint(sc_compare_mount_entry(&a, &b), <, 0);
	b = test_entry_1;
}

static void test_sc_clone_mount_entry_from_mntent()
{
	struct sc_mount_entry *entry =
	    sc_clone_mount_entry_from_mntent(&test_mnt_1);
	test_looks_like_test_entry_1(entry);
	g_assert_null(entry->next);

	struct sc_mount_entry *next = sc_get_next_and_free_mount_entry(entry);
	g_assert_null(next);
}

static void test_sc_sort_mount_entries()
{
	struct sc_mount_entry *list;

	// Sort an empty list, it should not blow up.
	list = NULL;
	sc_sort_mount_entries(&list);
	g_assert(list == NULL);

	// Create a list with two items in wrong order (backwards).
	struct sc_mount_entry entry_1 = test_entry_1;
	struct sc_mount_entry entry_2 = test_entry_2;
	list = &entry_2;
	entry_2.next = &entry_1;
	entry_1.next = NULL;

	// Sort the list
	sc_sort_mount_entries(&list);

	// Ensure that the linkage now follows the right order.
	g_assert(list == &entry_1);
	g_assert(entry_1.next == &entry_2);
	g_assert(entry_2.next == NULL);
}

// Scenario: there is nothing to do yet at all.
static void test_sc_compute_required_mount_changes__scenario0()
{
	struct sc_mount_entry *current = NULL;
	struct sc_mount_entry *desired = NULL;
	struct sc_mount_change change;

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_null(desired);
	g_assert_null(current);
	g_assert_cmpint(change.action, ==, SC_ACTION_NONE);
	g_assert_null(change.entry);
}

// Scenario: the current profile contains things but the desired profile does
// not. We should see two unmounts taking place. 
static void test_sc_compute_required_mount_changes__scenario1()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change change;

	test_write_lines("current.fstab",
			 test_entry_str_1, test_entry_str_2, NULL);
	test_write_lines("desired.fstab", NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_1(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_2(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_null(desired);
	g_assert_null(current);
	g_assert_cmpint(change.action, ==, SC_ACTION_NONE);
	g_assert_null(change.entry);
}

// Scenario: the current profile is empty but the desired profile
// contains two entries. We should see two mounts taking place.
static void test_sc_compute_required_mount_changes__scenario2()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change change;

	test_write_lines("current.fstab", NULL);
	test_write_lines("desired.fstab",
			 test_entry_str_1, test_entry_str_2, NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_1(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_2(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_null(desired);
	g_assert_null(current);
	g_assert_cmpint(change.action, ==, SC_ACTION_NONE);
	g_assert_null(change.entry);
}

// Scenario: the current profile contains one entry but the desired profile
// contains two entries. We should see one mount change (for the 2nd entry).
static void test_sc_compute_required_mount_changes__scenario3()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change change;

	test_write_lines("current.fstab", test_entry_str_1, NULL);
	test_write_lines("desired.fstab",
			 test_entry_str_1, test_entry_str_2, NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_2(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_null(desired);
	g_assert_null(current);
	g_assert_cmpint(change.action, ==, SC_ACTION_NONE);
	g_assert_null(change.entry);
}

// Scenario: the current profile contains one entry and the desired profile
// contains one entry but they are different. We should see the unmount
// followed by the mount.
static void test_sc_compute_required_mount_changes__scenario4()
{
	struct sc_mount_entry *current;
	struct sc_mount_entry *desired;
	struct sc_mount_change change;

	test_write_lines("current.fstab", test_entry_str_1, NULL);
	test_write_lines("desired.fstab", test_entry_str_2, NULL);

	current = sc_load_mount_profile("current.fstab");
	desired = sc_load_mount_profile("desired.fstab");
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     current);
	g_test_queue_destroy((GDestroyNotify) sc_free_mount_entry_list,
			     desired);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_UNMOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_1(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_cmpint(change.action, ==, SC_ACTION_MOUNT);
	g_assert_nonnull(change.entry);
	test_looks_like_test_entry_2(change.entry);

	sc_compute_required_mount_changes(&desired, &current, &change);
	g_assert_null(desired);
	g_assert_null(current);
	g_assert_cmpint(change.action, ==, SC_ACTION_NONE);
	g_assert_null(change.entry);
}

static void test_sc_mount_str2opt()
{
	g_assert_cmpint(sc_mount_str2opt(""), ==, 0);
	g_assert_cmpint(sc_mount_str2opt("ro"), ==, MS_RDONLY);
	g_assert_cmpint(sc_mount_str2opt("nosuid"), ==, MS_NOSUID);
	g_assert_cmpint(sc_mount_str2opt("nodev"), ==, MS_NODEV);
	g_assert_cmpint(sc_mount_str2opt("noexec"), ==, MS_NOEXEC);
	g_assert_cmpint(sc_mount_str2opt("sync"), ==, MS_SYNCHRONOUS);
	g_assert_cmpint(sc_mount_str2opt("remount"), ==, MS_REMOUNT);
	g_assert_cmpint(sc_mount_str2opt("mand"), ==, MS_MANDLOCK);
	g_assert_cmpint(sc_mount_str2opt("dirsync"), ==, MS_DIRSYNC);
	g_assert_cmpint(sc_mount_str2opt("noatime"), ==, MS_NOATIME);
	g_assert_cmpint(sc_mount_str2opt("nodiratime"), ==, MS_NODIRATIME);
	g_assert_cmpint(sc_mount_str2opt("bind"), ==, MS_BIND);
	g_assert_cmpint(sc_mount_str2opt("rbind"), ==, MS_BIND | MS_REC);
	g_assert_cmpint(sc_mount_str2opt("move"), ==, MS_MOVE);
	g_assert_cmpint(sc_mount_str2opt("silent"), ==, MS_SILENT);
	g_assert_cmpint(sc_mount_str2opt("acl"), ==, MS_POSIXACL);
	g_assert_cmpint(sc_mount_str2opt("private"), ==, MS_PRIVATE);
	g_assert_cmpint(sc_mount_str2opt("rprivate"), ==, MS_PRIVATE | MS_REC);
	g_assert_cmpint(sc_mount_str2opt("slave"), ==, MS_SLAVE);
	g_assert_cmpint(sc_mount_str2opt("rslave"), ==, MS_SLAVE | MS_REC);
	g_assert_cmpint(sc_mount_str2opt("shared"), ==, MS_SHARED);
	g_assert_cmpint(sc_mount_str2opt("rshared"), ==, MS_SHARED | MS_REC);
	g_assert_cmpint(sc_mount_str2opt("unbindable"), ==, MS_UNBINDABLE);
	g_assert_cmpint(sc_mount_str2opt("runbindable"), ==,
			MS_UNBINDABLE | MS_REC);

}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mount-entry/sc_load_mount_profile",
			test_sc_load_mount_profile);
	g_test_add_func("/mount-entry/sc_save_mount_profile",
			test_sc_save_mount_profile);
	g_test_add_func("/mount-entry/sc_compare_mount_entry",
			test_sc_compare_mount_entry);
	g_test_add_func("/mount-entry/test_sc_clone_mount_entry_from_mntent",
			test_sc_clone_mount_entry_from_mntent);
	g_test_add_func("/mount-entry/test_sort_mount_entries",
			test_sc_sort_mount_entries);
	g_test_add_func("/mount-entry/sc_compute_required_mount_changes/0",
			test_sc_compute_required_mount_changes__scenario0);
	g_test_add_func("/mount-entry/sc_compute_required_mount_changes/1",
			test_sc_compute_required_mount_changes__scenario1);
	g_test_add_func("/mount-entry/sc_compute_required_mount_changes/2",
			test_sc_compute_required_mount_changes__scenario2);
	g_test_add_func("/mount-entry/sc_compute_required_mount_changes/3",
			test_sc_compute_required_mount_changes__scenario3);
	g_test_add_func("/mount-entry/sc_compute_required_mount_changes/4",
			test_sc_compute_required_mount_changes__scenario4);
	g_test_add_func("/mount/sc_mount_str2opt", test_sc_mount_str2opt);
}
