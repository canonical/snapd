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
#include <ctype.h>

#include "utils.h"
#include "string-utils.h"
#include "cleanup-funcs.h"

bool sc_security_tag_validate(const char *security_tag,
			      const char *snap_instance,
			      const char *component_name)
{
	/* Don't even check overly long tags. */
	if (strlen(security_tag) > SNAP_SECURITY_TAG_MAX_LEN) {
		return false;
	}
	const char *whitelist_re =
	    "^snap\\.([a-z0-9](-?[a-z0-9])*(_[a-z0-9]{1,10})?)(\\.[a-zA-Z0-9](-?[a-zA-Z0-9])*|(\\+([a-z0-9](-?[a-z0-9])*))?\\.hook\\.[a-z](-?[a-z0-9])*)$";
	regex_t re;
	if (regcomp(&re, whitelist_re, REG_EXTENDED) != 0)
		die("can not compile regex %s", whitelist_re);

	// first capture is for verifying the full security tag, second capture
	// for verifying the snap_name is correct for this security tag, eighth capture
	// for verifying the component_name is correct for this security tag
	regmatch_t matches[8];
	int status =
	    regexec(&re, security_tag, sizeof matches / sizeof *matches,
		    matches, 0);
	regfree(&re);

	// Fail if no match or if snap name wasn't captured in the 2nd match group
	if (status != 0 || matches[1].rm_so < 0) {
		return false;
	}
	// if we expect a component name (a non-null string was passed in here),
	// then we need to make sure that the regex captured a component name
	if (component_name != NULL) {
		// don't allow empty component names, only allow NULL as an indication
		// that we don't expect a component name.
		if (strlen(component_name) == 0) {
			return false;
		}
		// fail if the security tag doesn't contain a component name and we
		// expected one
		if (matches[7].rm_so < 0) {
			return false;
		}

		size_t component_name_len = strlen(component_name);
		size_t len = matches[7].rm_eo - matches[7].rm_so;
		if (len != component_name_len
		    || strncmp(security_tag + matches[7].rm_so, component_name,
			       len) != 0) {
			return false;
		}
	} else if (matches[7].rm_so >= 0) {
		// fail if the security tag contains a component name and we didn't
		// expect one
		return false;
	}

	size_t len = matches[1].rm_eo - matches[1].rm_so;
	return len == strlen(snap_instance)
	    && strncmp(security_tag + matches[1].rm_so, snap_instance,
		       len) == 0;
}

bool sc_is_hook_security_tag(const char *security_tag)
{
	const char *whitelist_re =
	    "^snap\\.[a-z](-?[a-z0-9])*(_[a-z0-9]{1,10})?\\.(hook\\.[a-z](-?[a-z0-9])*)$";

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

static void sc_snap_or_component_name_validate(const char *snap_name,
					       bool is_component,
					       sc_error **errorp)
{
	// NOTE: This function should be synchronized with the two other
	// implementations: validate_snap_name and snap.ValidateName.
	sc_error *err = NULL;
	int err_code =
	    is_component ? SC_SNAP_INVALID_COMPONENT : SC_SNAP_INVALID_NAME;

	// Ensure that name is not NULL
	if (snap_name == NULL) {
		err = sc_error_init(SC_SNAP_DOMAIN, err_code,
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
		err = sc_error_init(SC_SNAP_DOMAIN, err_code,
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
						  err_code,
						  "snap name cannot end with a dash");
				goto out;
			}
			if (skip_one_char(&p, '-') > 0) {
				err =
				    sc_error_init(SC_SNAP_DOMAIN,
						  err_code,
						  "snap name cannot contain two consecutive dashes");
				goto out;
			}
			continue;
		}
		err = sc_error_init(SC_SNAP_DOMAIN, err_code,
				    "snap name must use lower case letters, digits or dashes");
		goto out;
	}
	if (!got_letter) {
		err = sc_error_init(SC_SNAP_DOMAIN, err_code,
				    "snap name must contain at least one letter");
		goto out;
	}
	if (n < 2) {
		err = sc_error_init(SC_SNAP_DOMAIN, err_code,
				    "snap name must be longer than 1 character");
		goto out;
	}
	if (n > SNAP_NAME_LEN) {
		err = sc_error_init(SC_SNAP_DOMAIN, err_code,
				    "snap name must be shorter than 40 characters");
		goto out;
	}

 out:
	sc_error_forward(errorp, err);
}

void sc_instance_name_validate(const char *instance_name, sc_error **errorp)
{
	// NOTE: This function should be synchronized with the two other
	// implementations: validate_instance_name and snap.ValidateInstanceName.
	sc_error *err = NULL;

	// Ensure that name is not NULL
	if (instance_name == NULL) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_NAME,
				  "snap instance name cannot be NULL");
		goto out;
	}

	if (strlen(instance_name) > SNAP_INSTANCE_LEN) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_NAME,
				  "snap instance name can be at most %d characters long",
				  SNAP_INSTANCE_LEN);
		goto out;
	}
	// instance name length + 1 extra overflow + 1 NULL
	char s[SNAP_INSTANCE_LEN + 1 + 1] = { 0 };
	strncpy(s, instance_name, sizeof(s) - 1);

	char *t = s;
	const char *snap_name = strsep(&t, "_");
	const char *instance_key = strsep(&t, "_");
	const char *third_separator = strsep(&t, "_");
	if (third_separator != NULL) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_NAME,
				  "snap instance name can contain only one underscore");
		goto out;
	}

	sc_snap_name_validate(snap_name, &err);
	if (err != NULL) {
		goto out;
	}
	// When the instance_name is a normal snap name, instance_key will be
	// NULL, so only validate instance_key when we found one.
	if (instance_key != NULL) {
		sc_instance_key_validate(instance_key, &err);
	}

 out:
	sc_error_forward(errorp, err);
}

void sc_instance_key_validate(const char *instance_key, sc_error **errorp)
{
	// NOTE: see snap.ValidateInstanceName for reference of a valid instance key
	// format
	sc_error *err = NULL;

	// Ensure that name is not NULL
	if (instance_key == NULL) {
		err = sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME,
				    "instance key cannot be NULL");
		goto out;
	}
	// This is a regexp-free routine hand-coding the following pattern:
	//
	// "^[a-z]{1,10}$"
	//
	// The only motivation for not using regular expressions is so that we don't
	// run untrusted input against a potentially complex regular expression
	// engine.
	int i = 0;
	for (i = 0; instance_key[i] != '\0'; i++) {
		if (islower(instance_key[i]) || isdigit(instance_key[i])) {
			continue;
		}
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_KEY,
				  "instance key must use lower case letters or digits");
		goto out;
	}

	if (i == 0) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_KEY,
				  "instance key must contain at least one letter or digit");
	} else if (i > SNAP_INSTANCE_KEY_LEN) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_INSTANCE_KEY,
				  "instance key must be shorter than 10 characters");
	}
 out:
	sc_error_forward(errorp, err);
}

void sc_snap_component_validate(const char *snap_component,
				const char *snap_instance, sc_error **errorp)
{
	sc_error *err = NULL;

	// ensure that name is not NULL
	if (snap_component == NULL) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT,
				  "snap component cannot be NULL");
		goto out;
	}

	const char *pos = strchr(snap_component, '+');
	if (pos == NULL) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT,
				  "snap component must contain a +");
		goto out;
	}

	size_t snap_name_len = pos - snap_component;
	if (snap_name_len > SNAP_NAME_LEN) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT,
				  "snap name must be shorter than 40 characters");
		goto out;
	}

	size_t component_name_len = strlen(pos + 1);
	if (component_name_len > SNAP_NAME_LEN) {
		err =
		    sc_error_init(SC_SNAP_DOMAIN, SC_SNAP_INVALID_COMPONENT,
				  "component name must be shorter than 40 characters");
		goto out;
	}

	char snap_name[SNAP_NAME_LEN + 1] = { 0 };
	strncpy(snap_name, snap_component, snap_name_len);

	char component_name[SNAP_NAME_LEN + 1] = { 0 };
	strncpy(component_name, pos + 1, component_name_len);

	sc_snap_or_component_name_validate(snap_name, true, &err);
	if (err != NULL) {
		goto out;
	}

	sc_snap_or_component_name_validate(component_name, true, &err);
	if (err != NULL) {
		goto out;
	}

	if (snap_instance != NULL) {
		char snap_name_in_instance[SNAP_NAME_LEN + 1] = { 0 };
		sc_snap_drop_instance_key(snap_instance, snap_name_in_instance,
					  sizeof snap_name_in_instance);

		if (strcmp(snap_name, snap_name_in_instance) != 0) {
			err =
			    sc_error_init(SC_SNAP_DOMAIN,
					  SC_SNAP_INVALID_COMPONENT,
					  "snap name in component must match snap name in instance");
			goto out;
		}
	}

 out:
	sc_error_forward(errorp, err);
}

void sc_snap_name_validate(const char *snap_name, sc_error **errorp)
{
	sc_snap_or_component_name_validate(snap_name, false, errorp);
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
	sc_string_split(instance_name, '_', snap_name, snap_name_size,
			instance_key, instance_key_size);
}

void sc_snap_split_snap_component(const char *snap_component,
				  char *snap_name, size_t snap_name_size,
				  char *component_name,
				  size_t component_name_size)
{
	sc_string_split(snap_component, '+', snap_name, snap_name_size,
			component_name, component_name_size);
}
