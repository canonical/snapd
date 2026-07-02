/*
 * Copyright (C) 2026 Canonical Ltd
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

/*
 * snapd-tool-wrap is a generic multi-call wrapper for internal snapd tools
 * (e.g. snap-preseed, snapd-apparmor). It is hardlinked under each tool name
 * in /usr/lib/snapd/<tool-name>.
 *
 * At runtime, it uses basename(argv[0]) to determine the tool name, preserves
 * it as argv[0] for display (ps, top), inserts it at argv[1] as the dispatch
 * signal, and execv()s into the snapd multi-call binary.
 *
 * Go dispatch in cmd/snapd/main.go checks argv[1] for known tool names before
 * falling through to the argv[0]-based daemon/CLI dispatch.
 */

#include <libgen.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#define SNAPD_PATH LIBEXECDIR "/snapd"

int main(int argc, char **argv) {
    char *tool = basename(argv[0]);
    char *new_argv[argc + 2];
    int i;

    /* argv[0] = tool name (for display in ps/top) */
    /* argv[1] = tool name (dispatch signal for Go code) */
    new_argv[0] = tool;
    new_argv[1] = tool;
    for (i = 1; i < argc; i++) {
        new_argv[i + 1] = argv[i];
    }
    new_argv[argc + 1] = NULL;

    execv(SNAPD_PATH, new_argv);
    /* execv only returns on error */
    perror("execv " SNAPD_PATH);
    return 1;
}
