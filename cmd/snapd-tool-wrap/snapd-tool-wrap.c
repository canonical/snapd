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
 * (e.g. snap-preseed, snapd-apparmor). It is symlinked, hardlinked or copied
 * under each tool name in /usr/lib/snapd/<tool-name>.
 *
 * At runtime, it uses basename(argv[0]) to determine the tool name, sets
 * argv[0]="snapd" (unambiguous dispatch), places the tool name at argv[1], and
 * execv()s into the snapd multi-call binary.
 *
 * The path to the snapd binary is derived at runtime from /proc/self/exe (i.e.
 * the directory containing this wrapper) rather than a hardcoded LIBEXECDIR.
 * This is essential for correct re-exec behavior as we're expecting the tool
 * wrapper to be placed in the same directory as a matching snapd binary.
 */

#include <libgen.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
    char self[PATH_MAX];
    char snapd_path[PATH_MAX];
    ssize_t n;
    char *slash;
    char *tool = basename(argv[0]);
    char *new_argv[argc + 2];
    int i;

    /* Derive the path to the snapd binary from our own location. This works
     * correctly both on the system (/usr/lib(exec)/snapd/snapd) and inside a
     * snapd snap (/snap/snapd/123/usr/lib/snapd/snapd). */
    n = readlink("/proc/self/exe", self, sizeof(self) - 1);
    if (n < 0) {
        perror("readlink /proc/self/exe");
        return 1;
    }
    self[n] = '\0';

    slash = strrchr(self, '/');
    if (slash == NULL) {
        fprintf(stderr, "snapd-tool-wrap: cannot determine directory of %s\n", self);
        return 1;
    }
    /* Replace everything after the last slash with "snapd". */
    slash[1] = '\0';
    if (snprintf(snapd_path, sizeof(snapd_path), "%ssnapd", self) >= (int)sizeof(snapd_path)) {
        fprintf(stderr, "snapd-tool-wrap: snapd path too long\n");
        return 1;
    }

    /* argv[0] = "snapd" — unambiguous dispatch signal */
    /* argv[1] = tool name — identifies the tool to the Go dispatch */
    new_argv[0] = "snapd";
    new_argv[1] = tool;
    for (i = 1; i < argc; i++) {
        new_argv[i + 1] = argv[i];
    }
    new_argv[argc + 1] = NULL;

    execv(snapd_path, new_argv);
    /* execv only returns on error */
    perror(snapd_path);
    return 1;
}
