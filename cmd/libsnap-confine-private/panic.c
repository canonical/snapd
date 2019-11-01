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

#include "panic.h"

#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static sc_panic_exit_fn panic_exit_fn = NULL;
static sc_panic_msg_fn panic_msg_fn = NULL;

void sc_panic(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    sc_panicv(fmt, ap);
    va_end(ap);
}

void sc_panicv(const char *fmt, va_list ap) {
    int errno_copy = errno;

    if (panic_msg_fn != NULL) {
        panic_msg_fn(fmt, ap, errno_copy);
    } else {
        vfprintf(stderr, fmt, ap);
        if (errno != 0) {
            fprintf(stderr, ": %s\n", strerror(errno_copy));
        } else {
            fprintf(stderr, "\n");
        }
    }

    if (panic_exit_fn != NULL) {
        panic_exit_fn();
    }
    exit(1);
}

sc_panic_exit_fn sc_set_panic_exit_fn(sc_panic_exit_fn fn) {
    sc_panic_exit_fn old = panic_exit_fn;
    panic_exit_fn = fn;
    return old;
}

sc_panic_msg_fn sc_set_panic_msg_fn(sc_panic_msg_fn fn) {
    sc_panic_msg_fn old = panic_msg_fn;
    panic_msg_fn = fn;
    return old;
}
