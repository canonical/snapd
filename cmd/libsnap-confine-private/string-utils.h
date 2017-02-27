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
 * Safer version of snprintf.
 *
 * This version dies on any error condition.
 **/
__attribute__ ((format(printf, 3, 4)))
int sc_must_snprintf(char *str, size_t size, const char *format, ...);

/**
 * Append a string to a buffer containing a string.
 *
 * This version is fully aware of the destination buffer and is extra careful
 * not to overflow it. If any argument is NULL a buffer overflow is detected
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
 * Neither character cannot be the string terminator.
 **/
size_t sc_string_append_char_pair(char *dst, size_t dst_size, char c1, char c2);

/**
 * Initialize a string (make it empty).
 *
 * Initialize a string as empty, ensuring buf is non-NULL buf_size is > 0.
 **/
void sc_string_init(char *buf, size_t buf_size);

#endif
