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

#ifndef SNAP_CONFINE_FACTS_H
#define SNAP_CONFINE_FACTS_H

#include <stdbool.h>
#include <stddef.h>

/* Facts are stored as multi-line strings using key=value syntax */

/**
 * Directory where facts are stored by snapd.
 *
 * The directory *may* be absent.
**/
#define SC_FACT_DIR "/var/lib/snapd/facts"

/**
 * Load facts from a given file.
 *
 * The file must contain key=value facts, one per line. The file may be absent,
 * it is equivalent to an empty file. Facts are limited to, at most, 16KB of
 * data.
 *
 * The return value must by released by calling free(3).
 **/
char *sc_load_facts(const char *fname);

/** 
 * Find, and possibly copy, a fact with the given name.
 *
 * The return value is always the number of bytes needed to represent the
 * fact, including the terminating '\0' character or 0 if the fact was not
 * found.
 *
 * If a non-empty buffer is provided then up to n bytes of the fact are stored
 * in the buffer. At all times the buffer is terminated with the '\0'
 * character.
**/
size_t sc_query_fact(const char *facts, const char *name, char *buf, size_t n);

/**
 * Find the value of a boolean fact with a default value.
 *
 * The return value is the boolean interpretation of the fact with the given
 * name or the default_value if the fact was not available or was not the string
 * "true" or "false".
**/
bool sc_get_bool_fact(const char *facts, const char *name, bool default_value);

#endif
