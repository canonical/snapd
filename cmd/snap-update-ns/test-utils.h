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

#ifndef SC_SNAP_CONFINE_TEST_UTILS_H
#define SC_SNAP_CONFINE_TEST_UTILS_H

/**
 * Write a sequence of lines to the given file.
 *
 * Lines are provided as arguments. The list must be terminated with a NULL
 * pointer. Lines should not contain a trailing newline as that is added
 * automatically.
 *
 * The written file is automatically removed when the test terminates.
 **/
void sc_test_write_lines(const char *name, ...) __attribute__ ((sentinel));

/**
 * Remove a file created during testing.
 *
 * This function is compatible with GDestroyNotify. It just calls
 * remove(3) but has a different signature.
 **/
void sc_test_remove_file(const char *name);

#endif
