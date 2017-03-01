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

#ifndef SNAP_CONFINE_TEST_DATA_H
#define SNAP_CONFINE_TEST_DATA_H

#include "mount-entry.h"

extern const char *test_entry_str_1;
extern const char *test_entry_str_2;

extern const struct sc_mount_entry test_entry_1;
extern const struct sc_mount_entry test_entry_2;

extern const struct mntent test_mnt_1;
extern const struct mntent test_mnt_2;

void test_looks_like_test_entry_1(const struct sc_mount_entry *entry);
void test_looks_like_test_entry_2(const struct sc_mount_entry *entry);

#endif
