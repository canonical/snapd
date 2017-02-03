/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

#include "string-utils.h"

#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <string.h>

#include "utils.h"

bool sc_streq(const char *a, const char *b)
{
	if (!a || !b) {
		return false;
	}

	size_t alen = strlen(a);
	size_t blen = strlen(b);

	if (alen != blen) {
		return false;
	}

	return strncmp(a, b, alen) == 0;
}

bool sc_endswith(const char *str, const char *suffix)
{
	if (!str || !suffix) {
		return false;
	}

	size_t xlen = strlen(suffix);
	size_t slen = strlen(str);

	if (slen < xlen) {
		return false;
	}

	return strncmp(str - xlen + slen, suffix, xlen) == 0;
}

int sc_must_snprintf(char *str, size_t size, const char *format, ...)
{
	int n;

	va_list va;
	va_start(va, format);
	n = vsnprintf(str, size, format, va);
	va_end(va);

	if (n < 0 || n >= size)
		die("cannot format string: %s", str);

	return n;
}

size_t sc_string_append(char *dst, size_t dst_size, const char *str)
{
	// Set errno in case we die.
	errno = 0;
	if (dst == NULL) {
		die("cannot append string: buffer is NULL");
	}
	if (str == NULL) {
		die("cannot append string: string is NULL");
	}
	size_t dst_len = strnlen(dst, dst_size);
	if (dst_len == dst_size) {
		die("cannot append string: dst is unterminated");
	}

	size_t max_str_len = dst_size - dst_len;
	size_t str_len = strnlen(str, max_str_len);
	if (str_len == max_str_len) {
		die("cannot append string: str is too long or unterminated");
	}
	// Append the string
	memcpy(dst + dst_len, str, str_len);
	// Ensure we are terminated
	dst[dst_len + str_len] = '\0';
	// return the new size
	return strlen(dst);
}
