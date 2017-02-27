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
#include "config.h"
#include "snap.h"

#include <regex.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

#include "utils.h"
#include "string-utils.h"
#include "cleanup-funcs.h"

static regex_t sc_valid_snap_name_re;

bool verify_security_tag(const char *security_tag)
{
	// The executable name is of form:
	// snap.<name>.(<appname>|hook.<hookname>)
	// - <name> must start with lowercase letter, then may contain
	//   lowercase alphanumerics and '-'
	// - <appname> may contain alphanumerics and '-'
	// - <hookname must start with a lowercase letter, then may
	//   contain lowercase letters and '-'
	const char *whitelist_re =
	    "^snap\\.[a-z](-?[a-z0-9])*\\.([a-zA-Z0-9](-?[a-zA-Z0-9])*|hook\\.[a-z](-?[a-z])*)$";
	regex_t re;
	if (regcomp(&re, whitelist_re, REG_EXTENDED | REG_NOSUB) != 0)
		die("can not compile regex %s", whitelist_re);

	int status = regexec(&re, security_tag, 0, NULL, 0);
	regfree(&re);

	return (status == 0);
}

void sc_snap_name_validate(const char *snap_name, struct sc_error **errorp)
{
	struct sc_error *err = NULL;

	// Ensure that name is not NULL
	if (snap_name == NULL) {
		err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				    "snap name cannot be NULL");
		goto out;
	}
	// Ensure that name matches regular expression
	if (regexec(&sc_valid_snap_name_re, snap_name, 0, NULL, 0) != 0) {
		char *quote_buf __attribute__ ((cleanup(sc_cleanup_string))) =
		    NULL;
		size_t quote_buf_size = strlen(snap_name) * 4 + 3;

		quote_buf = calloc(1, quote_buf_size);
		if (quote_buf == NULL) {
			err =
			    sc_error_init_from_errno(errno,
						     "cannot allocate memory for quoted snap name");
			goto out;
		}

		sc_string_quote(quote_buf, quote_buf_size, snap_name);
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				  "snap name is not valid (%s)", quote_buf);
	}

 out:
	sc_error_forward(errorp, err);
}

static void __attribute__ ((constructor)) init_snap()
{
	// NOTE: this regular expression should be kept in sync with what is in
	// snap/validate.go, in the validSnapName variable.
	const char *name_re = "^([a-z0-9]+-?)*[a-z](-?[a-z0-9])*$";

	if (regcomp(&sc_valid_snap_name_re, name_re, REG_EXTENDED | REG_NOSUB)
	    != 0) {
		die("cannot compile regex %s", name_re);
	}
}

static void __attribute__ ((destructor)) fini_snap()
{
	regfree(&sc_valid_snap_name_re);
}
