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

#ifndef SNAP_CONFINE_STRING_UTILS_H
#define SNAP_CONFINE_STRING_UTILS_H

#include <stdbool.h>
#include <stddef.h>

/**
 * Check if two strings are equal.
 **/
bool sc_streq(const char *a, const char *b);

/**
 * Check if a string has a given suffix.
 **/
bool sc_endswith(const char *str, const char *suffix);

/**
 * Check if a string has a given prefix.
 **/
bool sc_startswith(const char *str, const char *prefix);

/**
 * Allocate and return a copy of a string.
**/
char *sc_strdup(const char *str);

/**
 * Safer version of snprintf.
 *
 * This version dies on any error condition.
 **/
__attribute__((format(printf, 3, 4)))
int sc_must_snprintf(char *str, size_t size, const char *format, ...);

/**
 * Append a string to a buffer containing a string.
 *
 * This version is fully aware of the destination buffer and is extra careful
 * not to overflow it. If any argument is NULL or a buffer overflow is detected
 * then the function dies.
 *
 * The buffers cannot overlap.
 **/
size_t sc_string_append(char *dst, size_t dst_size, const char *str);

/**
 * Append a single character to a buffer containing a string.
 *
 * This version is fully aware of the destination buffer and is extra careful
 * not to overflow it. If any argument is NULL or a buffer overflow is detected
 * then the function dies.
 *
 * The character cannot be the string terminator.
 *
 * The return value is the new length of the string.
 **/
size_t sc_string_append_char(char *dst, size_t dst_size, char c);

/**
 * Append a pair of characters to a buffer containing a string.
 *
 * This version is fully aware of the destination buffer and is extra careful
 * not to overflow it. If any argument is NULL or a buffer overflow is detected
 * then the function dies.
 *
 * Neither character can be the string terminator.
 *
 * The return value is the new length of the string.
 **/
size_t sc_string_append_char_pair(char *dst, size_t dst_size, char c1, char c2);

/**
 * Initialize a string (make it empty).
 *
 * Initialize a string as empty, ensuring buf is non-NULL buf_size is > 0.
 **/
void sc_string_init(char *buf, size_t buf_size);

/**
 * Quote a string so it is safe for printing.
 *
 * This function is fully aware of the destination buffer and is extra careful
 * not to overflow it. If any argument is NULL or a buffer overflow is detected
 * then the function dies.
 *
 * The function "quotes" the content of the given string into the given buffer.
 * The buffer must be of sufficient size. Apart from letters and digits and
 * some punctuation all characters are escaped using their hexadecimal escape
 * codes.
 *
 * As a practical consideration the buffer should be of the following capacity:
 * strlen(str) * 4 + 2 + 1; This corresponds to the most pessimistic escape
 * process (each character is escaped to a hexadecimal value like \x05, two
 * double-quote characters (one front, one rear) and the final string
 * terminator character.
 **/
void sc_string_quote(char *buf, size_t buf_size, const char *str);

/**
 * Split a string into two parts on the first occurrence of a delimiter.
 *
 * The size of prefix must be large enough to hold the prefix part of the
 * string, and the size of suffix must be large enough to hold the suffix part
 * of the string.
 **/
void sc_string_split(const char *string, char delimiter,
		     char *prefix, size_t prefix_size,
		     char *suffix, size_t suffix_size);

#endif
