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
#include "error.h"

// To get vasprintf
#define _GNU_SOURCE

#include "utils.h"

#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <string.h>

struct sc_error {
	// Error domain defines a scope for particular error codes.
	const char *domain;
	// Code differentiates particular errors for the programmer.
	// The code may be zero if the particular meaning is not relevant.
	int code;
	// Message carries a formatted description of the problem.
	char *msg;
};

static struct sc_error *sc_error_initv(const char *domain, int code,
				       const char *msgfmt, va_list ap)
{
	struct sc_error *err = calloc(1, sizeof *err);
	if (err == NULL) {
		die("cannot allocate memory for error object");
	}
	err->domain = domain;
	err->code = code;
	if (vasprintf(&err->msg, msgfmt, ap) == -1) {
		die("cannot format error message");
	}
	return err;
}

struct sc_error *sc_error_init(const char *domain, int code, const char *msgfmt,
			       ...)
{
	va_list ap;
	va_start(ap, msgfmt);
	struct sc_error *err = sc_error_initv(domain, code, msgfmt, ap);
	va_end(ap);
	return err;
}

struct sc_error *sc_error_init_from_errno(int errno_copy, const char *msgfmt,
					  ...)
{
	va_list ap;
	va_start(ap, msgfmt);
	struct sc_error *err =
	    sc_error_initv(SC_ERRNO_DOMAIN, errno_copy, msgfmt, ap);
	va_end(ap);
	return err;
}

const char *sc_error_domain(struct sc_error *err)
{
	if (err == NULL) {
		die("cannot obtain error domain from NULL error");
	}
	return err->domain;
}

int sc_error_code(struct sc_error *err)
{
	if (err == NULL) {
		die("cannot obtain error code from NULL error");
	}
	return err->code;
}

const char *sc_error_msg(struct sc_error *err)
{
	if (err == NULL) {
		die("cannot obtain error message from NULL error");
	}
	return err->msg;
}

void sc_error_free(struct sc_error *err)
{
	if (err != NULL) {
		free(err->msg);
		err->msg = NULL;
		free(err);
	}
}

void sc_cleanup_error(struct sc_error **ptr)
{
	sc_error_free(*ptr);
	*ptr = NULL;
}

void sc_die_on_error(struct sc_error *error)
{
	if (error != NULL) {
		if (strcmp(sc_error_domain(error), SC_ERRNO_DOMAIN) == 0) {
			// Set errno just before the call to die() as it is used internally
			errno = sc_error_code(error);
			die("%s", sc_error_msg(error));
		} else {
			errno = 0;
			die("%s", sc_error_msg(error));
		}
	}
}

void sc_error_forward(struct sc_error **recipient, struct sc_error *error)
{
	if (recipient != NULL) {
		*recipient = error;
	} else {
		sc_die_on_error(error);
	}
}

bool sc_error_match(struct sc_error *error, const char *domain, int code)
{
	if (domain == NULL) {
		die("cannot match error to a NULL domain");
	}
	if (error == NULL) {
		return false;
	}
	return strcmp(sc_error_domain(error), domain) == 0
	    && sc_error_code(error) == code;
}
