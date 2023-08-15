/*
 * Copyright (C) 2018 Canonical Ltd
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
#include "selinux-support.h"
#include "config.h"

#include <selinux/context.h>
#include <selinux/selinux.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

static void sc_freecon(char **ctx) {
    if (ctx != NULL && *ctx != NULL) {
        freecon(*ctx);
        *ctx = NULL;
    }
}

static void sc_context_free(context_t *ctx) {
    if (ctx != NULL && *ctx != NULL) {
        context_free(*ctx);
        *ctx = NULL;
    }
}

/**
 * Set security context for the snap.
 *
 * Sets up SELinux context transition to unconfined_service_t.
 **/
int sc_selinux_set_snap_execcon(void) {
    if (is_selinux_enabled() < 1) {
        debug("SELinux not enabled");
        return 0;
    }

    char *ctx_str SC_CLEANUP(sc_freecon) = NULL;
    if (getcon(&ctx_str) < 0) {
        die("cannot obtain current SELinux process context");
    }
    debug("current SELinux process context: %s", ctx_str);

    context_t ctx SC_CLEANUP(sc_context_free) = context_new(ctx_str);
    if (ctx == NULL) {
        die("cannot create SELinux context from context string %s", ctx_str);
    }

    /* freed by context_free(ctx) */
    const char *ctx_type = context_type_get(ctx);

    if (ctx_type == NULL) {
        die("cannot obtain type from SELinux context string %s", ctx_str);
    }

    if (sc_streq(ctx_type, "snappy_confine_t")) {
        /* We are running under a targeted policy which ended up transitioning
         * to snappy_confine_t domain, at this point we are right before
         * executing snap-exec. However we do not have a full SELinux support
         * for services running in snaps, only the snapd bits and helpers are
         * covered by the policy.
         *
         * At this point transition to the unconfined_service_t domain (allowed
         * by snap_confine_t policy) upon the next exec() call.
         */
        if (context_type_set(ctx, "unconfined_service_t") != 0) {
            die("cannot update SELinux context %s type to unconfined_service_t", ctx_str);
        }

        /* freed by context_free(ctx) */
        const char *new_ctx_str = context_str(ctx);
        if (new_ctx_str == NULL) {
            die("cannot obtain updated SELinux context string");
        }
        if (setexeccon(new_ctx_str) < 0) {
            die("cannot set SELinux exec context to %s", new_ctx_str);
        }
        debug("SELinux context after next exec: %s", new_ctx_str);
    }

    return 0;
}
