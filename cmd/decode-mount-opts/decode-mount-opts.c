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

#include <stdio.h>
#include <stdlib.h>

#include "../libsnap-confine-private/mount-opt.h"

int main(int argc, char *argv[])
{
	if (argc != 2) {
		printf("usage: decode-mount-opts OPT\n");
		return 0;
	}
	char *end;
	unsigned long mountflags = strtoul(argv[1], &end, 0);
	if (*end != '\0') {
		fprintf(stderr, "cannot parse given argument as a number\n");
		return 1;
	}
	char buf[1000] = {0};
	printf("%#lx is %s\n", mountflags, sc_mount_opt2str(buf, sizeof buf, mountflags));
	return 0;
}
