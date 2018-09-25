/*
 * Copyright (C) 2018 Canonical Ltd
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

#include "facts.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cleanup-funcs.h"
#include "string-utils.h"
#include "utils.h"

char *sc_load_facts(const char *fname)
{
	FILE *f __attribute__ ((cleanup(sc_cleanup_file))) = NULL;
	char *buf_copy;
	size_t nread;
	char buf[16 * 1024 + 1];

	f = fopen(fname, "rt");
	if (f == NULL) {
		if (errno == ENOENT) {
			return NULL;
		}
		die("cannot open facts file %s", fname);
	}

	nread = fread(buf, 1, sizeof buf, f);
	if (!feof(f)) {
		die("cannot load facts larger than 16KB");
	}

	buf_copy = malloc(nread + 1);
	if (buf_copy == NULL) {
		die("cannot allocate memory for facts");
	}

	memcpy(buf_copy, buf, nread);
	buf_copy[nread] = '\0';
	return buf_copy;
}

size_t sc_query_fact(const char *facts, const char *name, char *buf, size_t n)
{
	const char *f_start, *f_end, *f_value_start, *f_next;
	size_t f_len, f_value_len, name_len;

	if (name == NULL || *name == '\0') {
		return 0;
	}
	name_len = strlen(name);

	/* Advance from one fact to the next. Each loop finds the next fact. */
	for (f_start = facts; f_start != NULL; f_start = f_next) {
		/* Facts are delimited with newlines, the last newline is optional. */
		f_next = strchr(f_start, '\n');
		if (f_next != NULL) {
			f_end = f_next;
			/* Step over the newline to look at the next fact. */
			f_next += 1;
		} else {
			f_end = f_start + strlen(f_start);
			/* f_next remains null, last iteration */
		}
		f_len = f_end - f_start;

		if (f_len < name_len + 1) {
			/* Skip entries shorter than "${name}=" */
			continue;
		}
		if ((strncmp(f_start, name, name_len) != 0)
		    || f_start[name_len] != '=') {
			/* Skip entries not starting with "${name}=" */
			continue;
		}
		/* The value starts just after "${name}=" */
		f_value_start = &f_start[name_len + 1];
		/* The length includes the terminating '\0' we wish to insert. */
		f_value_len = f_end - f_value_start + 1;

		if (buf != NULL && n > 0) {
			if (f_value_len < n) {
				/* Copy complete fact. */
				memcpy(buf, f_value_start, f_value_len);
				buf[f_value_len - 1] = '\0';
			} else {
				/* Copy truncated fact. */
				memcpy(buf, f_value_start, n);
				buf[n - 1] = '\0';
			}
		}

		/* Return the number of bytes needed to represent the value. */
		return f_value_len;
	}
	return 0;
}

bool sc_get_bool_fact(const char *facts, const char *name, bool default_value)
{
	/* Sufficient to represent "true" and "false" and the terminator. */
	char value[6];

	if (sc_query_fact(facts, name, value, sizeof value) > 0) {
		if (sc_streq(value, "true")) {
			return true;
		}
		if (sc_streq(value, "false")) {
			return false;
		}
	}
	return default_value;
}
