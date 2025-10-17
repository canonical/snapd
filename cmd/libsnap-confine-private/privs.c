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

#include "config.h"

#include "privs.h"

#include <errno.h>
#include <grp.h>
#include <linux/securebits.h>
#include <stdbool.h>
#include <sys/capability.h>
#include <sys/prctl.h>
#include <sys/types.h>
#include <unistd.h>

#include "cleanup-funcs.h"
#include "utils.h"

// Ubuntu 14.04 has a 4.4 kernel, but these macros are not defined
#ifndef PR_CAP_AMBIENT
#define PR_CAP_AMBIENT 47
#define PR_CAP_AMBIENT_IS_SET 1
#define PR_CAP_AMBIENT_RAISE 2
#define PR_CAP_AMBIENT_LOWER 3
#define PR_CAP_AMBIENT_CLEAR_ALL 4
#endif

void sc_cleanup_cap_t(cap_t *ptr) {
    if (ptr != NULL && *ptr != NULL) {
        cap_free(*ptr);
        *ptr = NULL;
    }
}

/* the same as sc_cleanup_cap_t but applicable to char* type */
static void sc_cleanup_cap_str(char **ptr) {
    if (ptr != NULL && *ptr != NULL) {
        cap_free(*ptr);
        *ptr = NULL;
    }
}

void sc_privs_drop(void) {
    /* TODO: this should use cap_set_mode(CAP_MODE_NOPRIV) for better effect,
     * but it's not supported by libcap 2.25 in 18.04 */
    cap_t working SC_CLEANUP(sc_cleanup_cap_t) = cap_init();
    if (working == NULL) {
        die("cannot allocate working caps set");
    }
    if (cap_set_proc(working) != 0) {
        die("cannot drop capabilities");
    }
}

void sc_debug_capabilities(const char *msg_prefix) {
    if (sc_is_debug_enabled()) {
        cap_t caps SC_CLEANUP(sc_cleanup_cap_t) = cap_get_proc();
        if (caps == NULL) {
            die("cannot obtain current capabilities");
        }
        char *caps_as_str SC_CLEANUP(sc_cleanup_cap_str) = cap_to_text(caps, NULL);
        if (caps_as_str == NULL) {
            die("cannot format capabilities string");
        }
        debug("%s: %s", msg_prefix, caps_as_str);
    }
}

int sc_cap_set_ambient(cap_value_t cap, cap_flag_value_t set) {
#if HAVE_CAP_SET_AMBIENT == 1
    return cap_set_ambient(cap, set);
#else
    // see:
    // https://git.kernel.org/pub/scm/libs/libcap/libcap.git/tree/libcap/cap_proc.c?id=31ed2fef38340e5d4ddc1e3d2a4449d3d046ff2d#n283
    int val;
    switch (set) {
        case CAP_SET:
            val = PR_CAP_AMBIENT_RAISE;
            break;
        case CAP_CLEAR:
            val = PR_CAP_AMBIENT_LOWER;
            break;
        default:
            errno = EINVAL;
            return -1;
    }
    return prctl(PR_CAP_AMBIENT, val, cap, 0, 0);
#endif
}

int sc_cap_reset_ambient(void) {
#if HAVE_CAP_SET_AMBIENT == 1
    return cap_reset_ambient();
#else
    // see:
    // https://git.kernel.org/pub/scm/libs/libcap/libcap.git/tree/libcap/cap_proc.c?id=31ed2fef38340e5d4ddc1e3d2a4449d3d046ff2d#n310
    return prctl(PR_CAP_AMBIENT, PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0);
#endif
}

void sc_cap_assert_permitted(cap_t current, const cap_value_t caps[], size_t caps_n) {
    for (size_t i = 0; i < caps_n; i++) {
        cap_value_t val = caps[i];
        cap_flag_value_t is_permitted = CAP_CLEAR;
        if (cap_get_flag(current, val, CAP_PERMITTED, &is_permitted) != 0) {
            die("cannot check capability value");
        }

        if (is_permitted == CAP_CLEAR) {
            char *name SC_CLEANUP(sc_cleanup_cap_str) = cap_to_name(val);
            char *current_text SC_CLEANUP(sc_cleanup_cap_str) = cap_to_text(current, NULL);
            die("required permitted capability %s not found in current capabilities:\n  %s", name, current_text);
        }
    }
}
