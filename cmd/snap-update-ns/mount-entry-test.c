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
	.entry = {
		  .mnt_fsname = "fsname-1",
		  .mnt_dir = "dir-1",
		  .mnt_type = "type-1",
		  .mnt_opts = "opts-1",
		  .mnt_freq = 1,
		  .mnt_passno = 2,
		  }
};

static void test_looks_like_test_entry_1(const struct sc_mount_entry *entry)
{
	g_assert_cmpstr(entry->entry.mnt_fsname, ==, "fsname-1");
	g_assert_cmpstr(entry->entry.mnt_dir, ==, "dir-1");
	g_assert_cmpstr(entry->entry.mnt_type, ==, "type-1");
	g_assert_cmpstr(entry->entry.mnt_opts, ==, "opts-1");
	g_assert_cmpint(entry->entry.mnt_freq, ==, 1);
	g_assert_cmpint(entry->entry.mnt_passno, ==, 2);
}

static const struct sc_mount_entry test_entry_2 = {
	.entry = {
		  .mnt_fsname = "fsname-2",
		  .mnt_dir = "dir-2",
		  .mnt_type = "type-2",
		  .mnt_opts = "opts-2",
		  .mnt_freq = 3,
		  .mnt_passno = 4,
		  }
};

static void test_looks_like_test_entry_2(const struct sc_mount_entry *entry)
{
	g_assert_cmpstr(entry->entry.mnt_fsname, ==, "fsname-2");
	g_assert_cmpstr(entry->entry.mnt_dir, ==, "dir-2");
	g_assert_cmpstr(entry->entry.mnt_type, ==, "type-2");
	g_assert_cmpstr(entry->entry.mnt_opts, ==, "opts-2");
	g_assert_cmpint(entry->entry.mnt_freq, ==, 3);
	g_assert_cmpint(entry->entry.mnt_passno, ==, 4);
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

static void test_remove_file(const char *name)
{
	remove(name);
}

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

	// Cast-away the const qualifier. This just calls unlink and we don't
	// modify the name in any way. This way the signature is compatible with
	// that of GDestroyNotify.
	g_test_queue_destroy((GDestroyNotify) test_remove_file, (char *)name);
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

static void test_sc_load_mount_profile__no_such_file()
{
	struct sc_mount_entry *fstab
	    __attribute__ ((cleanup(sc_cleanup_mount_entry_list))) = NULL;
	fstab = sc_load_mount_profile("test.does-not-exist.fstab");
	g_assert_null(fstab);
}

static void test_sc_save_mount_profile()
{
	struct sc_mount_entry entry_1 = test_entry_1;
	struct sc_mount_entry entry_2 = test_entry_2;
	entry_1.next = &entry_2;
	entry_2.next = NULL;

	// We can save the profile defined above.
	sc_save_mount_profile(&entry_1, "test.fstab");

	// Cast-away the const qualifier. This just calls unlink and we don't
	// modify the name in any way. This way the signature is compatible with
	// that of GDestroyNotify.
	g_test_queue_destroy((GDestroyNotify) test_remove_file,
			     (char *)"test.fstab");

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

static void test_sc_clone_mount_entry_from_mntent()
{
	struct sc_mount_entry *entry =
	    sc_clone_mount_entry_from_mntent(&test_mnt_1);
	test_looks_like_test_entry_1(entry);
	g_assert_null(entry->next);

	struct sc_mount_entry *next = sc_get_next_and_free_mount_entry(entry);
	g_assert_null(next);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mount-entry/sc_load_mount_profile",
			test_sc_load_mount_profile);
	g_test_add_func("/mount-entry/sc_load_mount_profile/no_such_file",
			test_sc_load_mount_profile__no_such_file);
	g_test_add_func("/mount-entry/sc_save_mount_profile",
			test_sc_save_mount_profile);
	g_test_add_func("/mount-entry/test_sc_clone_mount_entry_from_mntent",
			test_sc_clone_mount_entry_from_mntent);
}
