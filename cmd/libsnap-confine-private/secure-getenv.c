/*
 * Copyright (C) 2016 Canonical Ltd
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
#include "secure-getenv.h"

#include <stdlib.h>
#include <sys/auxv.h>

#ifndef HAVE_SECURE_GETENV
char *secure_getenv(const char *name) {
    unsigned long secure = getauxval(AT_SECURE);
    if (secure != 0) {
        return NULL;
    }
    return getenv(name);
}
#endif  // ! HAVE_SECURE_GETENV
