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
#endif				// HAVE_CONFIG_H

#include <stdlib.h>
#include <stdio.h>
#include <sys/types.h>
#include <dirent.h>

// SC_CLEANUP will run the given cleanup function when the variable next
// to it goes out of scope.
#define SC_CLEANUP(n) __attribute__((cleanup(n)))

/**
 * Free a dynamically allocated string.
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_string))).
 **/
void sc_cleanup_string(char **ptr);

/**
 * Close an open file.
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_file))).
 **/
void sc_cleanup_file(FILE ** ptr);

/**
 * Close an open file with endmntent(3)
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_endmntent))).
 **/
void sc_cleanup_endmntent(FILE ** ptr);

/**
 * Close an open directory with closedir(3)
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_closedir))).
 **/
void sc_cleanup_closedir(DIR ** ptr);

/**
 * Close an open file descriptor with close(2)
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_close))).
 **/
void sc_cleanup_close(int *ptr);

#endif
