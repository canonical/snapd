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
 * snap-cli-wrap is an explicit entrypoint to the snap CLI invoked either
 * directly through /usr/bin/snap or through a special symlink under
 * $SNAP_BIN_DIR.
 *
 * Its only purpose is to act as a policy attachment point acted on during
 * exec(), should a given host system ship an extended security policy covering
 * the functionality of the snap command (e.g. the SELinux policy distributed
 * with the snapd package).
 */

#include <stdio.h>
#include <unistd.h>

#define SNAPD_PATH LIBEXECDIR "/snapd"

int main(int argc, char **argv) {
    /* preserve unchanged argv[0], which could be 'snap' if acting as the CLI
     * tool, or a magic symlink at /snap/bin/<app-name>  */
    execv(SNAPD_PATH, argv);
    /* execv only returns on error */
    perror("execv " SNAPD_PATH);
    return 1;
}
