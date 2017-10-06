/*
 * Copyright (C) 2016 Canonical Ltd
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

#ifndef SC_SNAP_CONFINE_ARGS_H
#define SC_SNAP_CONFINE_ARGS_H

#include <stdbool.h>

#include "../libsnap-confine-private/error.h"

/**
 * Error domain for errors related to argument parsing.
 **/
#define SC_ARGS_DOMAIN "args"

enum {
	/**
	 * Error indicating that the command line arguments could not be parsed
	 * correctly and usage message should be displayed to the user.
	 **/
	SC_ARGS_ERR_USAGE = 1,
};

/**
 * Opaque structure describing command-line arguments to snap-confine.
 **/
struct sc_args;

/**
 * Parse command line arguments for snap-confine.
 *
 * Snap confine understands very specific arguments.
 *
 * The argument vector can begin with "ubuntu-core-launcher" (with an optional
 * path) which implies that the first arctual argument (argv[1]) is a copy of
 * argv[2] and can be discarded.
 *
 * The argument vector is scanned, left to right, to look for switches that
 * start with the minus sign ('-'). Recognized options are stored and
 * memorized. Unrecognized options return an appropriate error object.
 *
 * Currently only one option is understood, that is "--version". It is simply
 * scanned, memorized and discarded. The presence of this switch can be
 * retrieved with sc_args_is_version_query().
 *
 * After all the option switches are scanned it is expected to scan two more
 * arguments: the security tag and the name of the executable to run.  An error
 * object is returned when those is missing.
 *
 * Both argc and argv are modified so the caller can look at the first unparsed
 * argument at argc[0]. This is only done if argument parsing is successful.
 **/
__attribute__ ((warn_unused_result))
struct sc_args *sc_nonfatal_parse_args(int *argcp, char ***argvp,
				       struct sc_error **errorp);

/**
 * Free the object describing command-line arguments to snap-confine.
 **/
void sc_args_free(struct sc_args *args);

/**
 * Cleanup an error with sc_args_free()
 *
 * This function is designed to be used with
 * SC_CLEANUP(sc_cleanup_args).
 **/
void sc_cleanup_args(struct sc_args **ptr);

/**
 * Check if snap-confine was invoked with the --version switch.
 **/
bool sc_args_is_version_query(struct sc_args *args);

/**
 * Check if snap-confine was invoked with the --classic switch.
 **/
bool sc_args_is_classic_confinement(struct sc_args *args);

/**
 * Get the security tag passed to snap-confine.
 *
 * The return value may be NULL if snap-confine was invoked with --version. It
 * is never NULL otherwise.
 *
 * The return value must not be freed(). It is bound to the lifetime of
 * the argument parser.
 **/
const char *sc_args_security_tag(struct sc_args *args);

/**
 * Get the executable name passed to snap-confine.
 *
 * The return value may be NULL if snap-confine was invoked with --version. It
 * is never NULL otherwise.
 *
 * The return value must not be freed(). It is bound to the lifetime of
 * the argument parser.
 **/
const char *sc_args_executable(struct sc_args *args);

/**
 * Get the name of the base snap to use.
 *
 * The return value must not be freed(). It is bound to the lifetime of
 * the argument parser.
 **/
const char *sc_args_base_snap(struct sc_args *args);

#endif
