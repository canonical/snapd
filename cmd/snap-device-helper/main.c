/*
 * Copyright (C) 2021 Canonical Ltd
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

#include "../libsnap-confine-private/utils.h"

#include "snap-device-helper.h"

int main(int argc, char *argv[]) {
    int old_invocation_detected = (argc >= 5);

    if ((argc != 2) && !old_invocation_detected) {
        die("incorrect number of arguments");
    }

    struct sdh_invocation inv = {
        .action = getenv("ACTION"),
        .tagname = old_invocation_detected ? argv[2] : argv[1],
        .major = getenv("MAJOR"),
        .minor = getenv("MINOR"),
        .subsystem = getenv("SUBSYSTEM"),
    };

    return snap_device_helper_run(&inv);
}
