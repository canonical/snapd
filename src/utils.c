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

void debug(const char *msg, ...)
{
	if (getenv("SNAP_CONFINE_DEBUG") == NULL)
		return;

	va_list va;
	va_start(va, msg);
	fprintf(stderr, "DEBUG: ");
	vfprintf(stderr, msg, va);
	fprintf(stderr, "\n");
	va_end(va);
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
	int n = -1;

	va_list va;
	va_start(va, format);
	n = vsnprintf(str, size, format, va);
	va_end(va);

	if (n < 0 || n >= size)
		die("failed to snprintf %s", str);

	return n;
}
