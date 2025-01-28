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
#ifndef SNAP_CONFINE_SECURE_GETENV_H
#define SNAP_CONFINE_SECURE_GETENV_H

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#ifndef HAVE_SECURE_GETENV
/**
 * Secure version of getenv()
 *
 * This version returns NULL if the process is running within a secure context.
 * This is exactly the same as the GNU extension to the standard library. It is
 * only used when glibc is not available.
 **/
char *secure_getenv(const char *name) __attribute__((nonnull(1), warn_unused_result));
#endif  // ! HAVE_SECURE_GETENV

#endif
