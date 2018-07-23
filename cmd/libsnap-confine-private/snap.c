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

#include <errno.h>
#include <regex.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>

#include "utils.h"
#include "string-utils.h"
#include "cleanup-funcs.h"

bool verify_security_tag(const char *security_tag, const char *snap_name)
{
	const char *whitelist_re =
	    "^snap\\.([a-z0-9](-?[a-z0-9])*)\\.([a-zA-Z0-9](-?[a-zA-Z0-9])*|hook\\.[a-z](-?[a-z])*)$";
	regex_t re;
	if (regcomp(&re, whitelist_re, REG_EXTENDED) != 0)
		die("can not compile regex %s", whitelist_re);

	// first capture is for verifying the full security tag, second capture
	// for verifying the snap_name is correct for this security tag
	regmatch_t matches[2];
	int status =
	    regexec(&re, security_tag, sizeof matches / sizeof *matches,
		    matches, 0);
	regfree(&re);

	// Fail if no match or if snap name wasn't captured in the 2nd match group
	if (status != 0 || matches[1].rm_so < 0) {
		return false;
	}

	size_t len = matches[1].rm_eo - matches[1].rm_so;
	return len == strlen(snap_name)
	    && strncmp(security_tag + matches[1].rm_so, snap_name, len) == 0;
}

bool sc_is_hook_security_tag(const char *security_tag)
{
	const char *whitelist_re =
	    "^snap\\.[a-z](-?[a-z0-9])*\\.(hook\\.[a-z](-?[a-z])*)$";

	regex_t re;
	if (regcomp(&re, whitelist_re, REG_EXTENDED | REG_NOSUB) != 0)
		die("can not compile regex %s", whitelist_re);

	int status = regexec(&re, security_tag, 0, NULL, 0);
	regfree(&re);

	return status == 0;
}

static int skip_lowercase_letters(const char **p)
{
	int skipped = 0;
	for (const char *c = *p; *c >= 'a' && *c <= 'z'; ++c) {
		skipped += 1;
	}
	*p = (*p) + skipped;
	return skipped;
}

static int skip_digits(const char **p)
{
	int skipped = 0;
	for (const char *c = *p; *c >= '0' && *c <= '9'; ++c) {
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
	// NOTE: This function should be synchronized with the two other
	// implementations: validate_snap_name and snap.ValidateName.
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
	const char *p = snap_name;
	if (skip_one_char(&p, '-')) {
		err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				    "snap name cannot start with a dash");
		goto out;
	}
	bool got_letter = false;
	int n = 0, m;
	for (; *p != '\0';) {
		if ((m = skip_lowercase_letters(&p)) > 0) {
			n += m;
			got_letter = true;
			continue;
		}
		if ((m = skip_digits(&p)) > 0) {
			n += m;
			continue;
		}
		if (skip_one_char(&p, '-') > 0) {
			n++;
			if (*p == '\0') {
				err =
				    sc_error_init(SC_SNAP_DOMAIN,
						  SC_SNAP_INVALID_NAME,
						  "snap name cannot end with a dash");
				goto out;
			}
			if (skip_one_char(&p, '-') > 0) {
				err =
				    sc_error_init(SC_SNAP_DOMAIN,
						  SC_SNAP_INVALID_NAME,
						  "snap name cannot contain two consecutive dashes");
				goto out;
			}
			continue;
		}
		err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				    "snap name must use lower case letters, digits or dashes");
		goto out;
	}
	if (!got_letter) {
		err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				    "snap name must contain at least one letter");
	}
	if (n > 40) {
		err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				    "snap name must be shorter than 40 characters");
	}

 out:
	sc_error_forward(errorp, err);
}

void sc_snap_drop_instance_key(const char *instance_name, char *snap_name,
			       size_t snap_name_size)
{
	sc_snap_split_instance_name(instance_name, snap_name, snap_name_size,
				    NULL, 0);
}

void sc_snap_split_instance_name(const char *instance_name, char *snap_name,
				 size_t snap_name_size, char *instance_key,
				 size_t instance_key_size)
{
	if (instance_name == NULL) {
		die("internal error: cannot split instance name when it is unset");
	}
	if (snap_name == NULL && instance_key == NULL) {
		die("internal error: cannot split instance name when both snap name and instance key are unset");
	}

	const char *pos = strchr(instance_name, '_');
	const char *instance_key_start = "";
	size_t snap_name_len = 0;
	size_t instance_key_len = 0;
	if (pos == NULL) {
		snap_name_len = strlen(instance_name);
	} else {
		snap_name_len = pos - instance_name;
		instance_key_start = pos + 1;
		instance_key_len = strlen(instance_key_start);
	}

	if (snap_name != NULL) {
		if (snap_name_len >= snap_name_size) {
			die("snap name buffer too small");
		}

		memcpy(snap_name, instance_name, snap_name_len);
		snap_name[snap_name_len] = '\0';
	}

	if (instance_key != NULL) {
		if (instance_key_len >= instance_key_size) {
			die("instance key buffer too small");
		}
		memcpy(instance_key, instance_key_start, instance_key_len);
		instance_key[instance_key_len] = '\0';
	}
}
