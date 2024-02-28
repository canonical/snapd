/*
 * Copyright (C) 2019 Canonical Ltd
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

#ifndef SNAP_CONFINE_INFOFILE_H
#define SNAP_CONFINE_INFOFILE_H

#include <stdio.h>

#include "../libsnap-confine-private/error.h"

/**
 * sc_infofile_get_key extracts a single value of a key=value pair from a given
 * stream.
 *
 * On success the return value is zero and err_out, if not NULL, value is
 * dereferenced and set to NULL. On failure the return value is -1 is and
 * detailed error information is stored by dereferencing err_out. If an error
 * occurs and err_out is NULL then the program dies, printing the error message.
 **/
int sc_infofile_get_key(FILE *stream, const char *key, char **value, sc_error **err_out);

/**
 * sc_infofile_get_ini_section_key extracts a single value of a key=value pair
 * from a given ini section of the stream.
 *
 * On success the return value is zero and err_out, if not NULL, value is
 * dereferenced and set to NULL. On failure the return value is -1 is and
 * detailed error information is stored by dereferencing err_out. If an error
 * occurs and err_out is NULL then the program dies, printing the error message.
 **/
int sc_infofile_get_ini_section_key(FILE *stream, const char *section, const char *key, char **value,
                                    sc_error **err_out);

#endif
