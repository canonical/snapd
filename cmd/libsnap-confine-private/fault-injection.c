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

#include "fault-injection.h"

#ifdef _ENABLE_FAULT_INJECTION

#include <stdlib.h>
#include <string.h>

struct sc_fault {
    const char *name;
    struct sc_fault *next;
    sc_fault_fn fn;
    struct sc_fault_state state;
};

static struct sc_fault *sc_faults = NULL;

bool sc_faulty(const char *name, void *ptr) {
    for (struct sc_fault *fault = sc_faults; fault != NULL; fault = fault->next) {
        if (strcmp(name, fault->name) == 0) {
            bool is_faulty = fault->fn(&fault->state, ptr);
            fault->state.ncalls++;
            return is_faulty;
        }
    }
    return false;
}

void sc_break(const char *name, sc_fault_fn fn) {
    struct sc_fault *fault = calloc(1, sizeof *fault);
    if (fault == NULL) {
        abort();
    }
    fault->name = name;
    fault->next = sc_faults;
    fault->fn = fn;
    fault->state.ncalls = 0;
    sc_faults = fault;
}

void sc_reset_faults(void) {
    struct sc_fault *next_fault;
    for (struct sc_fault *fault = sc_faults; fault != NULL; fault = next_fault) {
        next_fault = fault->next;
        free(fault);
    }
    sc_faults = NULL;
}

#else  // ifndef _ENABLE_FAULT_INJECTION

bool sc_faulty(const char *name, void *ptr) { return false; }

#endif  // ifndef _ENABLE_FAULT_INJECTION
