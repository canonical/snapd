/*
 * Copyright (C) 2020 Canonical Ltd
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
    // Signal to "snap run" that we are ready to get a debugger attached. When a
    // debugger gets attached it will stop the binary at whatever point the
    // binary is executing. So we cannot have clever code here that e.g. waits
    // for a debugger to get attached because that code would also get
    // stoppped/debugged by that debugger and that would be confusing for the
    // user.
    //
    // once a debugger is attached we expect it to send:
    //  "continue; signal SIGCONT"
    raise(SIGSTOP);

    // signal gdb to stop here
    printf("\n\n");
    printf("Welcome to `snap run --gdbserver`.\n");
    printf("You are right before your application is execed():\n");
    printf("- set any options you may need\n");
    printf("- (optionally) set a breakpoint in 'main'\n");
    printf("- use 'cont' to start\n");
    printf("\n\n");
    raise(SIGTRAP);

    const char *executable = argv[1];
    execv(executable, (char *const *)&argv[1]);
    perror("execv failed");
    // very different exit code to make an execve failure easy to distinguish
    return 101;
}
