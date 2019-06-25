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

#include "infofile.h"

#include <errno.h>
#include <stdarg.h>
#include <string.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/error.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

int sc_infofile_query(FILE *stream, sc_error **err_out, ...) {
    va_list ap;
    va_start(ap, err_out);
    sc_error *err = NULL;

    fpos_t start_pos;
    if (fgetpos(stream, &start_pos) < 0) {
        err = sc_error_init_from_errno(errno, "cannot determine stream position");
        goto out;
    }

    size_t line_size = 0;
    char *line_buf SC_CLEANUP(sc_cleanup_string) = NULL;
    for (;;) { /* This loop advances through the keys we are looking for. */
        const char *key = va_arg(ap, const char *);
        if (key == NULL) {
            break;
        }
        char **value = va_arg(ap, char **);
        if (value == NULL) {
            err = sc_error_init(SC_INTERNAL_DOMAIN, 0, "no storage provided for key %s", key);
            goto out;
        }
        *value = NULL;
        size_t key_len = strlen(key);
        if (fsetpos(stream, &start_pos) < 0) {
            err = sc_error_init_from_errno(errno, "cannot set stream position");
            goto out;
        }
        for (;;) { /* This loop advances through subsequent lines. */
            errno = 0;
            ssize_t nread = getline(&line_buf, &line_size, stream);
            if (nread < 0 && errno != 0) {
                err = sc_error_init_from_errno(errno, "cannot read another line");
                goto out;
            }
            if (nread <= 0) {
                break; /* There is nothing more to read. */
            }
            /* Guard against malformed input that may contain NUL bytes that
             * would confuse the code below. */
            if (memchr(line_buf, '\0', nread) != NULL) {
                err = sc_error_init(SC_INTERNAL_DOMAIN, 0, "read line contains embedded NUL byte");
                goto out;
            }
            /* Skip lines shorter than the key length. They cannot match our
             * key. The extra byte ensures that we can look for the equals sign
             * ('='). Note that at this time nread cannot be negative. */
            if ((size_t)nread < key_len + 1) {
                continue;
            }
            /* Replace the newline character, if any, with the NUL byte. */
            if (nread > 0 && line_buf[nread - 1] == '\n') {
                line_buf[nread - 1] = '\0';
                nread--;
            }
            /* If the prefix of the line is the search key followed by the
             * equals sign then this is a matching entry. Copy it to the
             * provided pointer, if any, and stop searching. */
            if (strstr(line_buf, key) == line_buf && line_buf[key_len] == '=') {
                *value = sc_strdup(line_buf + key_len + 1);
                break;
            }
        }
    }
    va_end(ap);
    if (fsetpos(stream, &start_pos) < 0) {
        err = sc_error_init_from_errno(errno, "cannot set stream position");
        goto out;
    }

out:
    sc_error_forward(err_out, err);
    return err != NULL ? -1 : 0;
}
