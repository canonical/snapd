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

#include "../libsnap-confine-private/secure-getenv.h"

#include "xauth.h"

#include <memory.h>
#include <stdlib.h>
#include <stdio.h>
#include <unistd.h>

#include <linux/limits.h>

static char *xauth_data = NULL;
static long xauth_data_length = 0;

void sc_xauth_load_from_env(void)
{
	const char *xauth_path = getenv("XAUTHORITY");
	if (!xauth_path)
		return;

	FILE *f = fopen(xauth_path, "rb");
	if (!f)
		return;

	fseek(f, 0, SEEK_END);
	long length = ftell(f);
	fseek(f, 0, SEEK_SET);

	xauth_data = (char*) malloc(sizeof(char) * length);
	if (!xauth_data) {
		fclose(f);
		return;
	}

	if (fread(xauth_data, 1, length, f) != length) {
		free(xauth_data);
		xauth_data = NULL;
	} else {
		xauth_data_length = length;
	}

	fclose(f);
}

void sc_xauth_populate(void)
{
	if (xauth_data == NULL)
		return;

	char name[] = "/tmp/xauth.XXXXXX";
	int fd = mkstemp(name);
	if (fd < 0 || write(fd, xauth_data, xauth_data_length) != xauth_data_length) {
		// FIXME print warning or die?
	}

	free(xauth_data);
	xauth_data = NULL;
	xauth_data_length = 0;

	char fd_path[PATH_MAX], path[PATH_MAX];
	snprintf(fd_path, PATH_MAX, "/proc/self/fd/%d", fd);
	memset(path, 0, sizeof(path));
	if (readlink(fd_path, path, sizeof(path)-1) > 0)
		setenv("XAUTHORITY", path, 1);

	close(fd);
}
