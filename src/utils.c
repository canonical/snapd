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
#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "utils.h"

void die(const char *msg, ...)
{
	va_list va;
	va_start(va, msg);
	vfprintf(stderr, msg, va);
	va_end(va);

	if (errno != 0) {
		perror(". errmsg");
	} else {
		fprintf(stderr, "\n");
	}
	exit(1);
}

bool error(const char *msg, ...)
{
	va_list va;
	va_start(va, msg);
	vfprintf(stderr, msg, va);
	va_end(va);

	return false;
}

struct sc_bool_name {
	const char *text;
	bool value;
};

static const struct sc_bool_name sc_bool_names[] = {
	{"yes", true},
	{"no", false},
	{"1", true},
	{"0", false},
	{"", false},
};

/**
 * Convert string to a boolean value.
 *
 * The return value is 0 in case of success or -1 when the string cannot be
 * converted correctly. In such case errno is set to indicate the problem and
 * the value is not written back to the caller-supplied pointer.
 **/
static int str2bool(const char *text, bool * value)
{
	if (value == NULL) {
		errno = EFAULT;
		return -1;
	}
	if (text == NULL) {
		*value = false;
		return 0;
	}
	for (int i = 0; i < sizeof sc_bool_names / sizeof *sc_bool_names; ++i) {
		if (strcmp(text, sc_bool_names[i].text) == 0) {
			*value = sc_bool_names[i].value;
			return 0;
		}
	}
	errno = EINVAL;
	return -1;
}

/**
 * Get an environment variable and convert it to a boolean.
 *
 * Supported values are those of str2bool(), namely "yes", "no" as well as "1"
 * and "0". All other values are treated as false and a diagnostic message is
 * printed to stderr.
 **/
static bool getenv_bool(const char *name)
{
	const char *str_value = getenv(name);
	bool value;
	if (str2bool(str_value, &value) < 0) {
		if (errno == EINVAL) {
			fprintf(stderr,
				"WARNING: unrecognized value of environment variable %s (expected yes/no or 1/0)\n",
				name);
			return false;
		} else {
			die("cannot convert value of environment variable %s to a boolean", name);
		}
	}
	return value;
}

void debug(const char *msg, ...)
{
	if (getenv_bool("SNAP_CONFINE_DEBUG")) {
		va_list va;
		va_start(va, msg);
		fprintf(stderr, "DEBUG: ");
		vfprintf(stderr, msg, va);
		fprintf(stderr, "\n");
		va_end(va);
	}
}

void write_string_to_file(const char *filepath, const char *buf)
{
	debug("write_string_to_file %s %s", filepath, buf);
	FILE *f = fopen(filepath, "w");
	if (f == NULL)
		die("fopen %s failed", filepath);
	if (fwrite(buf, strlen(buf), 1, f) != 1)
		die("fwrite failed");
	if (fflush(f) != 0)
		die("fflush failed");
	if (fclose(f) != 0)
		die("fclose failed");
}

int must_snprintf(char *str, size_t size, const char *format, ...)
{
	int n;

	va_list va;
	va_start(va, format);
	n = vsnprintf(str, size, format, va);
	va_end(va);

	if (n < 0 || n >= size)
		die("failed to snprintf %s", str);

	return n;
}
