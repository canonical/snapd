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
#include <stdbool.h>
#include <string.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/error.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

/**
 * sc_infofile_scanner_state represents the state of the scanner.
 *
 * The fields, lineno, key and value are read-only and are meant to be consumed
 * by the scanner callback function. The fields caller_state and stop can be
 * modified by the scanner callback function to alter the caller state and to
 * stop further scanning, respectively.
 **/
typedef struct sc_infofile_scanner_state {
    /* in variables */
    int lineno;
    const char *key;
    const char *value;
    /* out variables */
    void *caller_state;
    bool stop;
} sc_infofile_scanner_state;

/**
 * sc_infofile_scanner_fn is a callback type that assists sc_infofile_scan.
 *
 * The state is the same value that was provided to sc_infofile_scan and can
 * be used by the caller to pass a structure or anything else that makes sense
 * to retrieve useful information later.
 *
 * Both the key and the value strings are pointing into a temporary buffer and
 * are NUL terminated.  The callback function must either use them in-place
 * (e.g. mark their presence) or perform a copy in case the values need to
 * outlive the call to sc_infofile_scan.
 *
 * The function prototype includes err_out and int return code, which behave
 * exactly the same as in sc_infofile_scan, that is, return value is zero on
 * success, -1 on failure. In both cases err_out is set to either NULL or an
 * error object. If an error object cannot be stored the program dies.  In
 * practice sc_infofile_scan always provides an error receiver so that the
 * error can be forwarded to the caller.
 **/
typedef int (*sc_infofile_scanner_fn)(sc_infofile_scanner_state *scanner_state, sc_error **err_out);

/**
 * sc_infofile_scanner_conf represents the configuration of the scanner.
 *
 * The configuration is comprised of the FILE stream to scan, the scanner function
 * as well as the caller state that is provided by the caller and conveyed into the
 * scanner function.
 **/
typedef struct sc_infofile_scanner_conf {
    FILE *stream;
    sc_infofile_scanner_fn scanner_fn;
    void *caller_state;
} sc_infofile_scanner_conf;

/**
 * sc_infofile_scan performs linear scan of a given stream, extracting
 * key=value pairs and passing them along to the scanner function.
 *
 * The stream is scanned exactly once, using internally managed buffer. The
 * buffer is reused as key/value storage for the purpose of the scanner
 * function. The values provided to the scanner function must be copied if they
 * need to outlive the lifetime of the call into the scanner.
 *
 * Each line must be of the format key=value where key and value are arbitrary
 * strings, excluding the NUL byte which would be confusing in traditional C
 * strings.
 *
 * On success the return value is zero and err_out, if not NULL, is deferences
 * and set to NULL.  On failure the return value is -1 is and detailed error
 * information is stored by dereferencing err_out.  If an error occurs and
 * err_out is NULL then the program dies, printing the error message.
 **/
static int sc_infofile_scan(sc_infofile_scanner_conf *scanner_conf, sc_error **err_out);

/**
 * sc_infofile_get_key_state represents caller state for sc_infofile_get_key.
 *
 * The state gets passed to sc_infofile_scan and is used to convey the key
 * that is being looked for as well as the value that was found.
 **/
typedef struct sc_infofile_get_key_state {
    const char *wanted_key;
    char *stored_value;
} sc_infofile_get_key_state;

/**
 * sc_infofile_get_key_scanner is the scanner callback for sc_infofile_get_key.
 *
 * The callback unpacks the scanner state and if a key is found, stores the value
 * into the caller state and stops the scanning process.
 **/
static int sc_infofile_get_key_scanner(sc_infofile_scanner_state *scanner_state, sc_error **err_out) {
    sc_error *err = NULL;
    if (scanner_state == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_BUG, "scanner_state cannot be NULL");
        goto out;
    }
    if (scanner_state->key == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_BUG, "scanner_state->key cannot be NULL");
        goto out;
    }
    if (scanner_state->value == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_BUG, "scanner_state->value cannot be NULL");
        goto out;
    }
    sc_infofile_get_key_state *caller_state = scanner_state->caller_state;
    if (caller_state == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_BUG, "scanner_state->caller_state cannot be NULL");
        goto out;
    }
    if (caller_state->wanted_key == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_BUG, "caller_state->wanted_key cannot be NULL");
        goto out;
    }

    if (sc_streq(caller_state->wanted_key, scanner_state->key)) {
        caller_state->stored_value = sc_strdup(scanner_state->value);
        scanner_state->stop = true;
    }

out:
    return sc_error_forward(err_out, err);
}

int sc_infofile_get_key(FILE *stream, const char *key, char **value, sc_error **err_out) {
    sc_error *err = NULL;

    /* NOTE: stream is checked by sc_infofile_scan */
    if (key == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_API_MISUSE, "key cannot be NULL");
        goto out;
    }
    if (value == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_API_MISUSE, "value cannot be NULL");
        goto out;
    }

    sc_infofile_get_key_state get_key_state = {.wanted_key = key};
    sc_infofile_scanner_conf scanner_conf = {
        .stream = stream,
        .scanner_fn = sc_infofile_get_key_scanner,
        .caller_state = &get_key_state,
    };
    *value = NULL;
    if (sc_infofile_scan(&scanner_conf, &err) < 0) {
        goto out;
    }
    *value = get_key_state.stored_value;

out:
    return sc_error_forward(err_out, err);
}

static int sc_infofile_scan(sc_infofile_scanner_conf *scanner_conf, sc_error **err_out) {
    sc_error *err = NULL;
    size_t line_size = 0;
    char *line_buf SC_CLEANUP(sc_cleanup_string) = NULL;

    if (scanner_conf == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_API_MISUSE, "scanner_conf cannot be NULL");
        goto out;
    }
    if (scanner_conf->stream == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_API_MISUSE, "stream cannot be NULL");
        goto out;
    }
    if (scanner_conf->scanner_fn == NULL) {
        err = sc_error_init(SC_LIBSNAP_ERROR, SC_API_MISUSE, "scanner_fn cannot be NULL");
        goto out;
    }

    /* This loop advances through subsequent lines. */
    for (int lineno = 1;; ++lineno) {
        errno = 0;
        ssize_t nread = getline(&line_buf, &line_size, scanner_conf->stream);
        if (nread < 0 && errno != 0) {
            err = sc_error_init_from_errno(errno, "cannot read beyond line %d", lineno);
            goto out;
        }
        if (nread <= 0) {
            break; /* There is nothing more to read. */
        }
        /* NOTE: beyond this line the buffer is never empty. */

        /* Guard against malformed input that may contain NUL bytes that
         * would confuse the code below. */
        if (memchr(line_buf, '\0', nread) != NULL) {
            err = sc_error_init(SC_LIBSNAP_ERROR, 0, "line %d contains NUL byte", lineno);
            goto out;
        }
        /* Replace the newline character, if any, with the NUL byte. */
        if (line_buf[nread - 1] == '\n') {
            line_buf[nread - 1] = '\0';
        }
        /* Guard against malformed input that does not contain '=' byte */
        char *eq_ptr = memchr(line_buf, '=', nread);
        if (eq_ptr == NULL) {
            err = sc_error_init(SC_LIBSNAP_ERROR, 0, "line %d is not a key=value assignment", lineno);
            goto out;
        }
        /* Guard against malformed input with empty key. */
        if (eq_ptr == line_buf) {
            err = sc_error_init(SC_LIBSNAP_ERROR, 0, "line %d contains empty key", lineno);
            goto out;
        }
        /* Replace the first '=' with string terminator byte. */
        *eq_ptr = '\0';

        /* Call the scanner callback with the state for this location. */
        sc_infofile_scanner_state scanner_state = {
            .key = line_buf,
            .value = eq_ptr + 1,
            .lineno = lineno,
            .caller_state = scanner_conf->caller_state,
        };
        if (scanner_conf->scanner_fn(&scanner_state, &err) < 0) {
            goto out;
        }
        /* Stop scanning if the callback asked us to do so. */
        if (scanner_state.stop) {
            break;
        }
    }

out:
    return sc_error_forward(err_out, err);
}
