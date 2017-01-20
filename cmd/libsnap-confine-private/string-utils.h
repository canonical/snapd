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

/**
 * Check if two strings are equal.
 **/
bool sc_streq(const char *a, const char *b);

/**
 * Check if a string has a given suffix.
 **/
bool sc_endswith(const char *str, const char *suffix);

#endif
