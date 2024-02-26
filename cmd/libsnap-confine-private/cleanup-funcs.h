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

#ifndef SNAP_CONFINE_CLEANUP_FUNCS_H
#define SNAP_CONFINE_CLEANUP_FUNCS_H

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif  // HAVE_CONFIG_H

#include <dirent.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/types.h>

// SC_CLEANUP will run the given cleanup function when the variable next
// to it goes out of scope.
#define SC_CLEANUP(n) __attribute__((cleanup(n)))

/**
 * Free a dynamically allocated string.
 *
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is NULL.
 **/
void sc_cleanup_string(char **ptr);

/**
 * Free a dynamically allocated string vector.
 *
 * Both the vector itself and all the strings contained inside it will be
 * freed. It's assumed that the strings have been allocated with malloc().
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is NULL.
 */
void sc_cleanup_deep_strv(char ***ptr);

/**
 * Shallow free a dynamically allocated string vector.
 *
 * The strings in the vector will not be freed.
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is NULL.
 */
void sc_cleanup_shallow_strv(const char ***ptr);

/**
 * Close an open file.
 *
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is NULL.
 **/
void sc_cleanup_file(FILE **ptr);

/**
 * Close an open file with endmntent(3)
 *
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is NULL.
 **/
void sc_cleanup_endmntent(FILE **ptr);

/**
 * Close an open directory with closedir(3)
 *
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is NULL.
 **/
void sc_cleanup_closedir(DIR **ptr);

/**
 * Close an open file descriptor with close(2)
 *
 * This function is designed to be used with SC_CLEANUP() macro.
 * The variable MUST be initialized for correct operation.
 * The safe initialisation value is -1.
 **/
void sc_cleanup_close(int *ptr);

#endif
