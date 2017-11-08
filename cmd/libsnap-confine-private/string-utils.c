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

	if (n < 0 || (size_t) n >= size)
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

size_t sc_string_append_char(char *dst, size_t dst_size, char c)
{
	// Set errno in case we die.
	errno = 0;
	if (dst == NULL) {
		die("cannot append character: buffer is NULL");
	}
	size_t dst_len = strnlen(dst, dst_size);
	if (dst_len == dst_size) {
		die("cannot append character: dst is unterminated");
	}
	size_t max_str_len = dst_size - dst_len;
	if (max_str_len < 2) {
		die("cannot append character: not enough space");
	}
	if (c == 0) {
		die("cannot append character: cannot append string terminator");
	}
	// Append the character and terminate the string.
	dst[dst_len + 0] = c;
	dst[dst_len + 1] = '\0';
	// Return the new size
	return dst_len + 1;
}

size_t sc_string_append_char_pair(char *dst, size_t dst_size, char c1, char c2)
{
	// Set errno in case we die.
	errno = 0;
	if (dst == NULL) {
		die("cannot append character pair: buffer is NULL");
	}
	size_t dst_len = strnlen(dst, dst_size);
	if (dst_len == dst_size) {
		die("cannot append character pair: dst is unterminated");
	}
	size_t max_str_len = dst_size - dst_len;
	if (max_str_len < 3) {
		die("cannot append character pair: not enough space");
	}
	if (c1 == 0 || c2 == 0) {
		die("cannot append character pair: cannot append string terminator");
	}
	// Append the two characters and terminate the string.
	dst[dst_len + 0] = c1;
	dst[dst_len + 1] = c2;
	dst[dst_len + 2] = '\0';
	// Return the new size
	return dst_len + 2;
}

void sc_string_init(char *buf, size_t buf_size)
{
	errno = 0;
	if (buf == NULL) {
		die("cannot initialize string, buffer is NULL");
	}
	if (buf_size == 0) {
		die("cannot initialize string, buffer is too small");
	}
	buf[0] = '\0';
}

void sc_string_quote(char *buf, size_t buf_size, const char *str)
{
	if (str == NULL) {
		die("cannot quote string: string is NULL");
	}
	const char *hex = "0123456789abcdef";
	// NOTE: this also checks buf/buf_size sanity so that we don't have to.
	sc_string_init(buf, buf_size);
	sc_string_append_char(buf, buf_size, '"');
	for (unsigned char c; (c = *str) != 0; ++str) {
		switch (c) {
			// Pass ASCII letters and digits unmodified.
		case '0' ... '9':
		case 'A' ... 'Z':
		case 'a' ... 'z':
			// Pass most of the punctuation unmodified.
		case ' ':
		case '!':
		case '#':
		case '$':
		case '%':
		case '&':
		case '(':
		case ')':
		case '*':
		case '+':
		case ',':
		case '-':
		case '.':
		case '/':
		case ':':
		case ';':
		case '<':
		case '=':
		case '>':
		case '?':
		case '@':
		case '[':
		case '\'':
		case ']':
		case '^':
		case '_':
		case '`':
		case '{':
		case '|':
		case '}':
		case '~':
			sc_string_append_char(buf, buf_size, c);
			break;
			// Escape special whitespace characters.
		case '\n':
			sc_string_append_char_pair(buf, buf_size, '\\', 'n');
			break;
		case '\r':
			sc_string_append_char_pair(buf, buf_size, '\\', 'r');
			break;
		case '\t':
			sc_string_append_char_pair(buf, buf_size, '\\', 't');
			break;
		case '\v':
			sc_string_append_char_pair(buf, buf_size, '\\', 'v');
			break;
			// Escape the escape character.
		case '\\':
			sc_string_append_char_pair(buf, buf_size, '\\', '\\');
			break;
			// Escape double quote character.
		case '"':
			sc_string_append_char_pair(buf, buf_size, '\\', '"');
			break;
			// Escape everything else as a generic hexadecimal escape string.
		default:
			sc_string_append_char_pair(buf, buf_size, '\\', 'x');
			sc_string_append_char_pair(buf, buf_size, hex[c >> 4],
						   hex[c & 15]);
			break;
		}
	}
	sc_string_append_char(buf, buf_size, '"');
}
