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

#ifndef SNAP_CONFINE_FAULT_INJECTION_H
#define SNAP_CONFINE_FAULT_INJECTION_H

#include <stdbool.h>

/**
 * Check for an injected fault.
 *
 * The name of the fault must match what was passed to sc_break(). The second
 * argument can be modified by the fault callback function. The return value
 * indicates if a fault was injected. It is assumed that once a fault was
 * injected the passed pointer was used to modify the state in useful way.
 *
 * When the pre-processor macro _ENABLE_FAULT_INJECTION is not defined this
 * function always returns false and does nothing at all.
 **/
bool sc_faulty(const char *name, void *ptr);

#ifdef _ENABLE_FAULT_INJECTION

struct sc_fault_state;

typedef bool (*sc_fault_fn)(struct sc_fault_state *state, void *ptr);

struct sc_fault_state {
    int ncalls;
};

/**
 * Inject a fault for testing.
 *
 * The name of the fault must match the expected calls to sc_faulty().  The
 * second argument is a callback that is invoked each time sc_faulty() is
 * called. It is designed to inspect an argument passed to sc_faulty() and as
 * well as the state of the fault injection point and return a boolean
 * indicating that a fault has occurred.
 *
 * After testing faults should be reset using sc_reset_faults().
 **/

void sc_break(const char *name, sc_fault_fn fn);

/**
 * Remove all the injected faults.
 **/
void sc_reset_faults(void);

#endif  // ifndef _ENABLE_FAULT_INJECTION

#endif
