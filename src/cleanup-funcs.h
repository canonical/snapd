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
#ifdef HAVE_SECCOMP
#include <seccomp.h>
#endif				// HAVE_SECCOMP

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

#ifdef HAVE_SECCOMP
/**
 * Release a seccomp context with seccomp_release(3)
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_seccomp_release))).
 **/
void sc_cleanup_seccomp_release(scmp_filter_ctx * ptr);
#endif				// HAVE_SECCOMP

#endif
