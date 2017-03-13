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

#include <ctype.h>
#include <errno.h>
#include <regex.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>

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

static int skip_lowercase_letters(const char **p)
{
	int skipped = 0;
	for (const char *c = *p; islower(*c); ++c) {
		skipped += 1;
	}
	*p = (*p) + skipped;
	return skipped;
}

static int skip_digits(const char **p)
{
	int skipped = 0;
	for (const char *c = *p; isdigit(*c); ++c) {
		skipped += 1;
	}
	*p = (*p) + skipped;
	return skipped;
}

static int skip_one_char(const char **p, char c)
{
	if (**p == c) {
		*p += 1;
		return 1;
	}
	return 0;
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
	// This is a regexp-free routine hand-codes the following pattern:
	//
	// "^([a-z0-9]+-?)*[a-z](-?[a-z0-9])*$"
	//
	// The only motivation for not using regular expressions is so that we
	// don't run untrusted input against a potentially complex regular
	// expression engine.
	const char *err_hint = NULL;
	const char *p = snap_name;
	if (skip_one_char(&p, '-')) {
		err_hint = "snap name cannot start with a dash";
		goto invalid;
	}
	bool got_letter = false;
	for (; *p != '\0';) {
		if (skip_lowercase_letters(&p) > 0) {
			got_letter = true;
			continue;
		}
		if (skip_digits(&p) > 0) {
			continue;
		}
		if (skip_one_char(&p, '-') > 0) {
			if (*p == '\0') {
				err_hint = "snap name cannot end with a dash";
				goto invalid;
			}
			if (skip_one_char(&p, '-') > 0) {
				err_hint =
				    "snap name cannot contain two consecutive dashes";
				goto invalid;
			}
			continue;
		}
		err_hint =
		    "snap name must use lower case letters, digits or dashes";
		goto invalid;
	}
	if (!got_letter) {
		err_hint = "snap name must contain at least one letter";
		goto invalid;
	}
	goto out;

 invalid:
	if (1) {
		char *quote_buf __attribute__ ((cleanup(sc_cleanup_string))) =
		    NULL;
		size_t quote_buf_size = strlen(snap_name) * 4 + 3;

		quote_buf = calloc(1, quote_buf_size);
		if (quote_buf == NULL) {
			err = sc_error_init_from_errno(errno,
						       "cannot allocate memory for quoted snap name");
			goto out;
		}
		sc_string_quote(quote_buf, quote_buf_size, snap_name);
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				  "%s (%s)", err_hint, quote_buf);
	}
 out:
	sc_error_forward(errorp, err);
}
