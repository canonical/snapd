/*
 * Copyright (C) 2019 Canonical Ltd
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
#include "snap-confine-invocation.h"

#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/snap-dir.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

void sc_init_invocation(sc_invocation *inv, const struct sc_args *args, const char *snap_instance,
                        const char *snap_component) {
    /* Snap instance name is conveyed via untrusted environment. It may be
     * unset (typically when experimenting with snap-confine by hand). It
     * must also be a valid snap instance name. */
    if (snap_instance == NULL) {
        die("cannot use NULL snap instance name");
    }
    sc_instance_name_validate(snap_instance, NULL);

    // snap_component may be NULL if what we're confining isn't from a component
    char *component_name = NULL;
    char component_name_buffer[SNAP_NAME_LEN + 1] = {0};
    if (snap_component != NULL) {
        sc_snap_component_validate(snap_component, snap_instance, NULL);

        sc_snap_split_snap_component(snap_component, NULL, 0, component_name_buffer, sizeof component_name_buffer);

        component_name = component_name_buffer;
    }

    /* The security tag is conveyed via untrusted command line. It must be
     * in agreement with snap instance name and must be a valid security
     * tag. */
    const char *security_tag = sc_args_security_tag(args);
    if (!sc_security_tag_validate(security_tag, snap_instance, component_name)) {
        die("security tag %s not allowed", security_tag);
    }

    /* The base snap name is conveyed via untrusted, optional, command line
     * argument. It may be omitted where it implies the "core" snap is the
     * base. */
    const char *base_snap_name = sc_args_base_snap(args);
    if (base_snap_name == NULL) {
        base_snap_name = "core";
    }
    sc_snap_name_validate(base_snap_name, NULL);

    /* The executable is conveyed via untrusted command line. It must be set
     * but cannot be validated further than that at this time. It might be
     * arguable to validate it to be snap-exec in one of the well-known
     * locations or one of the special-cases like strace / gdb but this is
     * not done at this time. */
    const char *executable = sc_args_executable(args);
    if (executable == NULL) {
        die("cannot run with NULL executable");
    }

    /* Instance name length + NULL termination */
    char snap_name[SNAP_NAME_LEN + 1] = {0};
    sc_snap_drop_instance_key(snap_instance, snap_name, sizeof snap_name);

    /* Invocation helps to pass relevant data to various parts of snap-confine. */
    memset(inv, 0, sizeof *inv);
    inv->base_snap_name = sc_strdup(base_snap_name);
    inv->orig_base_snap_name = sc_strdup(base_snap_name);
    inv->executable = sc_strdup(executable);
    inv->security_tag = sc_strdup(security_tag);
    inv->snap_instance = sc_strdup(snap_instance);
    inv->snap_component = snap_component != NULL ? sc_strdup(snap_component) : NULL;
    inv->snap_name = sc_strdup(snap_name);
    inv->classic_confinement = sc_args_is_classic_confinement(args);

    // construct rootfs_dir based on base_snap_name
    char mount_point[PATH_MAX] = {0};
    sc_must_snprintf(mount_point, sizeof mount_point, "%s/%s/current", sc_snap_mount_dir(NULL), inv->base_snap_name);
    inv->rootfs_dir = sc_strdup(mount_point);

    debug("security tag: %s", inv->security_tag);
    debug("executable:   %s", inv->executable);
    debug("confinement:  %s", inv->classic_confinement ? "classic" : "non-classic");
    debug("base snap:    %s", inv->base_snap_name);
}

void sc_cleanup_invocation(sc_invocation *inv) {
    if (inv != NULL) {
        sc_cleanup_string(&inv->snap_instance);
        sc_cleanup_string(&inv->snap_name);
        sc_cleanup_string(&inv->base_snap_name);
        sc_cleanup_string(&inv->orig_base_snap_name);
        sc_cleanup_string(&inv->security_tag);
        sc_cleanup_string(&inv->executable);
        sc_cleanup_string(&inv->rootfs_dir);
        sc_cleanup_string(&inv->snap_component);
        sc_cleanup_deep_strv(&inv->homedirs);
    }
}

void sc_check_rootfs_dir(sc_invocation *inv) {
    if (access(inv->rootfs_dir, F_OK) == 0) {
        return;
    }

    /* As a special fallback, allow the base snap to degrade from "core" to
     * "ubuntu-core". This is needed for the migration from old
     * ubuntu-core based systems to the new core.
     */
    if (sc_streq(inv->base_snap_name, "core")) {
        char mount_point[PATH_MAX] = {0};

        /* For "core" we can still use the ubuntu-core snap. This is helpful in
         * the migration path when new snap-confine runs before snapd has
         * finished obtaining the core snap. */
        sc_must_snprintf(mount_point, sizeof mount_point, "%s/%s/current", sc_snap_mount_dir(NULL), "ubuntu-core");
        if (access(mount_point, F_OK) == 0) {
            sc_cleanup_string(&inv->base_snap_name);
            inv->base_snap_name = sc_strdup("ubuntu-core");
            sc_cleanup_string(&inv->rootfs_dir);
            inv->rootfs_dir = sc_strdup(mount_point);
            debug("falling back to ubuntu-core instead of unavailable core snap");
            return;
        }
    }

    if (sc_streq(inv->base_snap_name, "core16")) {
        char mount_point[PATH_MAX] = {0};

        /* For "core16" we can still use the "core" snap. This is useful
         * to help people transition to core16 bases without requiring
         * twice the disk space.
         */
        sc_must_snprintf(mount_point, sizeof mount_point, "%s/%s/current", sc_snap_mount_dir(NULL), "core");
        if (access(mount_point, F_OK) == 0) {
            sc_cleanup_string(&inv->base_snap_name);
            inv->base_snap_name = sc_strdup("core");
            sc_cleanup_string(&inv->rootfs_dir);
            inv->rootfs_dir = sc_strdup(mount_point);
            debug("falling back to core instead of unavailable core16 snap");
            return;
        }
    }

    die("cannot locate base snap %s", inv->base_snap_name);
}

static char *read_homedirs_from_system_params(void) {
    FILE *f SC_CLEANUP(sc_cleanup_file) = NULL;
    f = fopen("/var/lib/snapd/system-params", "r");
    if (f == NULL) {
        return NULL;
    }

    char *line SC_CLEANUP(sc_cleanup_string) = NULL;
    size_t line_size = 0;
    while (getline(&line, &line_size, f) != -1) {
        if (sc_startswith(line, "homedirs=")) {
            return sc_strdup(line + (sizeof("homedirs=") - 1));
        }
    }
    return NULL;
}

void sc_invocation_init_homedirs(sc_invocation *inv) {
    char *config_line SC_CLEANUP(sc_cleanup_string) = read_homedirs_from_system_params();
    if (config_line == NULL) {
        return;
    }

    /* The homedirs setting is a comma-separated list. In order to allocate the
     * right number of strings, let's count how many commas we have.
     */
    int num_commas = 0;
    for (char *c = config_line; *c != '\0'; c++) {
        if (*c == ',') {
            num_commas++;
        }
    }

    /* We add *two* elements here: one is because of course the number of
     * actual homedirs is the number of commas plus one, and the extra one is
     * used as an end-of-array indicator. */
    inv->homedirs = calloc(num_commas + 2, sizeof(char *));
    if (inv->homedirs == NULL) {
        die("cannot allocate memory for homedirs");
    }

    // strtok_r needs a pointer to keep track of where it is in the
    // string.
    char *buf_saveptr = NULL;

    int current_index = 0;
    char *homedir = strtok_r(config_line, ",\n", &buf_saveptr);
    while (homedir != NULL) {
        if (homedir[0] == '\0') {
            // Deal with the case of an empty homedir line (e.g "homedirs=")
            continue;
        }
        inv->homedirs[current_index++] = sc_strdup(homedir);
        homedir = strtok_r(NULL, ",\n", &buf_saveptr);
    }

    // Store the actual amount of homedirs created
    inv->num_homedirs = current_index;
}
