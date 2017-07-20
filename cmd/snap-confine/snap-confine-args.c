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

#include "snap-confine-args.h"

#include <string.h>

#include "../libsnap-confine-private/utils.h"

struct sc_args {
	// The security tag that the application is intended to run with
	char *security_tag;
	// The executable that should be invoked
	char *executable;
	// Name of the base snap to use.
	char *base_snap;

	// Flag indicating that --version was passed on command line.
	bool is_version_query;
	// Flag indicating that --classic was passed on command line.
	bool is_classic_confinement;
};

struct sc_args *sc_nonfatal_parse_args(int *argcp, char ***argvp,
				       struct sc_error **errorp)
{
	struct sc_args *args = NULL;
	struct sc_error *err = NULL;

	if (argcp == NULL || argvp == NULL) {
		err = sc_error_init(SC_ARGS_DOMAIN, 0,
				    "cannot parse arguments, argcp or argvp is NULL");
		goto out;
	}
	// Use dereferenced versions of argcp and argvp for convenience.
	int argc = *argcp;
	char **const argv = *argvp;

	if (argc == 0 || argv == NULL) {
		err = sc_error_init(SC_ARGS_DOMAIN, 0,
				    "cannot parse arguments, argc is zero or argv is NULL");
		goto out;
	}
	// Sanity check, look for NULL argv entries.
	for (int i = 0; i < argc; ++i) {
		if (argv[i] == NULL) {
			err = sc_error_init(SC_ARGS_DOMAIN, 0,
					    "cannot parse arguments, argument at index %d is NULL",
					    i);
			goto out;
		}
	}

	args = calloc(1, sizeof *args);
	if (args == NULL) {
		die("cannot allocate memory for command line arguments object");
	}
	// Check if we're being called through the ubuntu-core-launcher symlink.
	// When this happens we want to skip the first positional argument as it is
	// the security tag repeated (legacy).
	bool ignore_first_tag = false;
	char *basename = strrchr(argv[0], '/');
	if (basename != NULL) {
		// NOTE: this is safe because we, at most, may move to the NUL byte
		// that compares to an empty string.
		basename += 1;
		if (strcmp(basename, "ubuntu-core-launcher") == 0) {
			ignore_first_tag = true;
		}
	}
	// Parse option switches.
	int optind;
	for (optind = 1; optind < argc; ++optind) {
		// Look at all the options switches that start with the minus sign ('-')
		if (argv[optind][0] != '-') {
			// On first non-switch argument break the loop. The next loop looks
			// just for non-option arguments. This ensures that options and
			// positional arguments cannot be mixed.
			break;
		}
		// Handle option switches
		if (strcmp(argv[optind], "--version") == 0) {
			args->is_version_query = true;
			// NOTE: --version short-circuits the parser to finish
			goto done;
		} else if (strcmp(argv[optind], "--classic") == 0) {
			args->is_classic_confinement = true;
		} else if (strcmp(argv[optind], "--base") == 0) {
			if (optind + 1 >= argc) {
				err =
				    sc_error_init(SC_ARGS_DOMAIN,
						  SC_ARGS_ERR_USAGE,
						  "Usage: snap-confine <security-tag> <executable>\n"
						  "\n"
						  "the --base option requires an argument");
				goto out;
			}
			if (args->base_snap != NULL) {
				err =
				    sc_error_init(SC_ARGS_DOMAIN,
						  SC_ARGS_ERR_USAGE,
						  "Usage: snap-confine <security-tag> <executable>\n"
						  "\n"
						  "the --base option can be used only once");
				goto out;

			}
			args->base_snap = strdup(argv[optind + 1]);
			if (args->base_snap == NULL) {
				die("cannot allocate memory for base snap name");
			}
			optind += 1;
		} else {
			// Report unhandled option switches
			err = sc_error_init(SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE,
					    "Usage: snap-confine <security-tag> <executable>\n"
					    "\n"
					    "unrecognized command line option: %s",
					    argv[optind]);
			goto out;
		}
	}

	// Parse positional arguments.
	//
	// NOTE: optind is not reset, we just continue from where we left off in
	// the loop above.
	for (; optind < argc; ++optind) {
		if (args->security_tag == NULL) {
			// The first positional argument becomes the security tag.
			if (ignore_first_tag) {
				// Unless we are called as ubuntu-core-launcher, then we just
				// swallow and ignore that security tag altogether.
				ignore_first_tag = false;
				continue;
			}
			args->security_tag = strdup(argv[optind]);
			if (args->security_tag == NULL) {
				die("cannot allocate memory for security tag");
			}
		} else if (args->executable == NULL) {
			// The second positional argument becomes the executable name.
			args->executable = strdup(argv[optind]);
			if (args->executable == NULL) {
				die("cannot allocate memory for executable name");
			}
			// No more positional arguments are required.
			// Stop the parsing process.
			break;
		}
	}

	// Verify that all mandatory positional arguments are present.
	// Ensure that we have the security tag
	if (args->security_tag == NULL) {
		err = sc_error_init(SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE,
				    "Usage: snap-confine <security-tag> <executable>\n"
				    "\n"
				    "application or hook security tag was not provided");
		goto out;
	}
	// Ensure that we have the executable name
	if (args->executable == NULL) {
		err = sc_error_init(SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE,
				    "Usage: snap-confine <security-tag> <executable>\n"
				    "\n" "executable name was not provided");
		goto out;
	}

	int i;
 done:
	// "shift" the argument vector left, except for argv[0], to "consume" the
	// arguments that were scanned / parsed correctly.
	for (i = 1; optind + i < argc; ++i) {
		argv[i] = argv[optind + i];
	}
	argv[i] = NULL;

	// Write the updated argc back, argv is never modified.
	*argcp = argc - optind;

 out:
	// Don't return anything in case of an error.
	if (err != NULL) {
		sc_cleanup_args(&args);
	}
	// Forward the error and return
	sc_error_forward(errorp, err);
	return args;
}

void sc_args_free(struct sc_args *args)
{
	if (args != NULL) {
		free(args->security_tag);
		args->security_tag = NULL;
		free(args->executable);
		args->executable = NULL;
		free(args->base_snap);
		args->base_snap = NULL;
		free(args);
	}
}

void sc_cleanup_args(struct sc_args **ptr)
{
	sc_args_free(*ptr);
	*ptr = NULL;
}

bool sc_args_is_version_query(struct sc_args *args)
{
	if (args == NULL) {
		die("cannot obtain version query flag from NULL argument parser");
	}
	return args->is_version_query;
}

bool sc_args_is_classic_confinement(struct sc_args * args)
{
	if (args == NULL) {
		die("cannot obtain classic confinement flag from NULL argument parser");
	}
	return args->is_classic_confinement;
}

const char *sc_args_security_tag(struct sc_args *args)
{
	if (args == NULL) {
		die("cannot obtain security tag from NULL argument parser");
	}
	return args->security_tag;
}

const char *sc_args_executable(struct sc_args *args)
{
	if (args == NULL) {
		die("cannot obtain executable from NULL argument parser");
	}
	return args->executable;
}

const char *sc_args_base_snap(struct sc_args *args)
{
	if (args == NULL) {
		die("cannot obtain base snap name from NULL argument parser");
	}
	return args->base_snap;
}
