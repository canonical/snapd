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

char *sc_must_stpcpy(char *buf, size_t buf_size, char *dest, const char *src)
{
	// Set errno in case we die.
	errno = 0;
	if (buf == NULL) {
		die("cannot append string: buffer is NULL");
	}
	if (dest == NULL) {
		die("cannot append string: destination is NULL");
	}
	if (src == NULL) {
		die("cannot append string: source is NULL");
	}
	// Sanity check, the code doesn't need buffers larger than a few KBs so
	// prevent corrupted or otherwise huge buffers from seeming "valid".
	if (buf_size >= 0xFFFF) {
		// NOTE: using %zd to format size_t as ssize_t which is more useful for
		// -1 and similar huge values and also is better to test as it is
		// independent of machine word size.
		die("cannot append string: buffer size (%zd) exceeds internal limit", (ssize_t) buf_size);

	}
	size_t src_len = strlen(src);
	// Sanity check, dest points to the inside of the buffer.
	if (dest == &buf[buf_size]) {
		die("cannot append string: destination points"
		    " to the end of the buffer");
	}
	if (dest > &buf[buf_size] && src_len > 0) {
		die("cannot append string: destination points"
		    " %td byte(s) beyond the buffer", dest - &buf[buf_size]);
	}
	if (dest < buf) {
		die("cannot append string: destination points"
		    " %td byte(s) in front of the buffer", buf - dest);
	}
	// Sanity check the new content fits the buffer.
	if (&dest[src_len] >= &buf[buf_size]) {
		die("cannot append string: buffer overflow of %td byte(s)",
		    &dest[src_len] - &buf[buf_size] + 1);
	}
	memcpy(dest, src, src_len + 1);
	return &dest[src_len + 1];
}
