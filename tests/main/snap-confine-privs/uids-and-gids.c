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
 */

#define _GNU_SOURCE
#include <errno.h>
#include <grp.h>
#include <pwd.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/types.h>
#include <unistd.h>

int main(int argc __attribute__((unused)), char* argv[] __attribute__((unused)))
{
    uid_t ruid, euid, suid;
    gid_t rgid, egid, sgid;
    if (getresuid(&ruid, &euid, &suid) < 0) {
        perror("cannot call getresuid");
        exit(1);
    }
    if (getresgid(&rgid, &egid, &sgid) < 0) {
        perror("cannot call getresgid");
        exit(1);
    }
    printf("ruid=%-5d euid=%-5d suid=%-5d rgid=%-5d egid=%-5d sgid=%-5d\n", ruid, euid, suid, rgid, egid, sgid);
    return 0;
}
