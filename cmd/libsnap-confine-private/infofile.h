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
 * sc_infofile_query extracts specific KEY=VALUE fields from a given file.
 *
 * The first argument is a stream which must support seeking. The function
 * scans the stream, starting from the current position, all the way until the
 * end of the stream, once for each key being extracted. At the end of the
 * operation the stream position is reset to the original location. This allows
 * repeated operation against a stream, as more information about the necessary
 * queries becomes known.
 *
 * The remaining function arguments form a NULL terminated list of pairs (key,
 * value_pointer) with types (const char *, char **). Each value pointer is
 * always set.
 *
 * If the key is missing the value is set to NULL. If the key is found the
 * value is set to a dynamically allocated copy of the value. The caller is
 * responsible for calling free on the returned values.
 *
 * The return value on success is zero. On failure -1 is returned and more
 * information is conveyed through the err_out pointer, which contains the
 * forwareded error. If the error cannot be forwarded the program dies,
 * printing the error message.
 **/
int sc_infofile_query(FILE *stream, sc_error **err_out, ...) __attribute__((sentinel));

#endif
