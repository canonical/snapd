/*
 * Copyright (C) 2017 Canonical Ltd
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

#ifndef SNAP_CONFINE_CONTEXT_SUPPORT_H
#define SNAP_CONFINE_CONTEXT_SUPPORT_H

#include "../libsnap-confine-private/error.h"

/**
 * Return snap cookie string for given snap.
 *
 * The context value is read from /var/lib/snapd/cookie/snap.<snapname>
 * file. The caller of the function takes the ownership of the returned cookie
 * string.
 * If the file cannot be read then an error is returned in errorp and
 * the function returns NULL.
 **/
char *sc_cookie_get_from_snapd(const char *snap_name, struct sc_error **errorp);

#endif
