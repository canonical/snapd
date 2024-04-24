/*
 * Copyright (C) 2016 Canonical Ltd
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

#ifndef SNAP_CONFINE_APPARMOR_SUPPORT_H
#define SNAP_CONFINE_APPARMOR_SUPPORT_H

#include <stdbool.h>

/**
 * Type of apparmor confinement.
 **/
enum sc_apparmor_mode {
	// The enforcement mode was not recognized.
	SC_AA_INVALID = -1,
	// The enforcement mode is not applicable because apparmor is disabled.
	SC_AA_NOT_APPLICABLE = 0,
	// The enforcement mode is "enforcing"
	SC_AA_ENFORCE = 1,
	// The enforcement mode is "complain"
	SC_AA_COMPLAIN,
	// The enforcement mode is "mixed"
	SC_AA_MIXED,
	// The enforcement mode is "kill"
	SC_AA_KILL,
};

/**
 * Data required to manage apparmor wrapper.
 **/
struct sc_apparmor {
	// The mode of enforcement. In addition to the two apparmor defined modes
	// can be also SC_AA_INVALID (unknown mode reported by apparmor) and
	// SC_AA_NOT_APPLICABLE (when we're not linked with apparmor).
	enum sc_apparmor_mode mode;
	// Flag indicating that the current process is confined.
	bool is_confined;
};

/**
 * Initialize apparmor support.
 *
 * This operation should be done even when apparmor support is disabled at
 * compile time. Internally the supplied structure is initialized based on the
 * information returned from aa_getcon(2) or if apparmor is disabled at compile
 * time, with built-in constants.
 *
 * The main action performed here is to check if snap-confine is currently
 * confined, this information is used later in sc_maybe_change_apparmor_hat()
 *
 * As with many functions in the snap-confine tree, all errors result in
 * process termination.
 **/
void sc_init_apparmor_support(struct sc_apparmor *apparmor);

/**
 * Maybe call aa_change_onexec(2)
 *
 * This function does nothing when apparmor support is not enabled at compile
 * time. If apparmor is enabled then profile change request is attempted.
 *
 * As with many functions in the snap-confine tree, all errors result in
 * process termination. As an exception, when SNAPPY_LAUNCHER_INSIDE_TESTS
 * environment variable is set then the process is not terminated.
 **/
void
sc_maybe_aa_change_onexec(struct sc_apparmor *apparmor, const char *profile);

/**
 * Maybe call aa_change_hat(2)
 *
 * This function does nothing when apparmor support is not enabled at compile
 * time. If apparmor is enabled then hat change is attempted.
 *
 * As with many functions in the snap-confine tree, all errors result in
 * process termination.
 **/
void
sc_maybe_aa_change_hat(struct sc_apparmor *apparmor,
		       const char *subprofile, unsigned long magic_token);

#endif
