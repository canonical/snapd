/*
 * Copyright (C) 2025 Canonical Ltd
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

#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/ptrace.h>
#include <unistd.h>

#include "../libsnap-confine-private/utils.h"

int main(int argc, char **argv) {
    if (sc_is_debug_enabled()) {
        for (int i = 0; i < argc; i++) {
            fprintf(stderr, "-%s-\n", argv[i]);
        }
    }
    if (argc < 2) {
        fprintf(stderr, "missing a command to execute");
        abort();
    }

    /* echo signal STOP to parent so that it knows when to attach strace */
    raise(SIGSTOP);

    const char *executable = argv[1];
    execv(executable, (char *const *)&argv[1]);
    perror("execv failed");
    // very different exit code to make an execve failure easy to distinguish
    return 101;
}
