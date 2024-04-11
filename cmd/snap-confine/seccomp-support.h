/*
 * Copyright (C) 2015-2017 Canonical Ltd
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
#ifndef SNAP_CONFINE_SECCOMP_SUPPORT_H
#define SNAP_CONFINE_SECCOMP_SUPPORT_H

#include <stdbool.h>

/** 
 * sc_apply_seccomp_profile_for_security_tag applies a seccomp profile to the
 * current process. The filter is loaded from a pre-compiled bpf bytecode
 * stored in "/var/lib/snap/seccomp/bpf" using the security tag and the
 * extension ".bin2". All components along that path must be owned by root and
 * cannot be writable by UNIX _other_.
 *
 * The security tag is shared with other parts of snapd.
 * For applications it is the string "snap.${SNAP_INSTANCE_NAME}.${app}".
 * For hooks it is "snap.${SNAP_INSTANCE_NAME}.hook.{hook_name}".
 *
 * Profiles must be present in the file-system. If a profile is not present
 * then several attempts are made, each coupled with a sleep period. Up 3600
 * seconds may elapse before the function gives up. Unless
 * $SNAP_CONFINE_MAX_PROFILE_WAIT environment variable dictates otherwise, the
 * default wait time is 120 seconds.
 *
 * A profile may contain valid BPF program or the string "@unrestricted\n".  In
 * the former case the profile is applied to the current process using
 * sc_apply_seccomp_filter. In the latter case no action takes place.
 *
 * The return value indicates if the process uses confinement or runs under the
 * special non-confining "@unrestricted" profile.
 **/
bool sc_apply_seccomp_profile_for_security_tag(const char *security_tag);

void sc_apply_global_seccomp_profile(void);

#endif
