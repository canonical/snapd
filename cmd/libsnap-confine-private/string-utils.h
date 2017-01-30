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
 * Safer version of stpcpy.
 *
 * This version is fully aware of the output buffer and is extra careful not to
 * overflow it. If any argument is NULL, buffer size is unrealistic (see below)
 * or a buffer overflow is detected then the function dies.
 *
 * Since snap-confine has very modest requirements buffers larger than 0xFFFF
 * are not allowed. This is meant as an extra sanity check to prevent
 * '-1'-sized buffers from allowing memory corruption to go on unnoticed.
 *
 * As a tip, the function should be used like this:
 *
 *   char buf[100];
 *   char *to = buf;
 *
 *   to = sc_must_stpcpy(buf, sizeof buf, to, "hello");
 *   to = sc_must_stpcpy(buf, sizeof buf, to, " ");
 *   sc_must_stpcpy(buf, sizeof buf, to, "world");
 *
 * The return value can be discarded when no more appending is necessary.
 **/
char *sc_must_stpcpy(char *buf, size_t buf_size, char *dest, const char *src);

#endif
