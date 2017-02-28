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
#include "test-utils.h"

#include <glib.h>

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>

void sc_test_remove_file(const char *name)
{
	int err = remove(name);
	g_assert_cmpint(err, ==, 0);
}

void sc_test_write_lines(const char *name, ...)
{
	FILE *f = fopen(name, "wt");
	g_assert_nonnull(f);

	va_list ap;
	va_start(ap, name);
	const char *line;
	while ((line = va_arg(ap, const char *)) != NULL) {
		fprintf(f, "%s\n", line);
	}
	va_end(ap);
	fclose(f);

	// Cast-away the const qualifier. This just calls unlink and we don't
	// modify the name in any way. This way the signature is compatible with
	// that of GDestroyNotify.
	g_test_queue_destroy((GDestroyNotify) sc_test_remove_file,
			     (char *)name);
}
