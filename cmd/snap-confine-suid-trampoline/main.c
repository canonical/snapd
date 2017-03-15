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

#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include "config.h"		// for SNAP_MOUNT_PATH

/**
 * The goal of the snap-confine-suid-trampoline program is to assist snapd in
 * running snap-confine from the core snap. To do this correctly we need to use
 * the dynamic linker from the core snap as the host system may not have the
 * right libraries. 
 *
 * The trampoline program is intended to be compiled to a static setuid root
 * binary. Once executed it unconditionally runs the linker from the core snap
 * to run snap-confine from the core snap (and to resolve shared libraries
 * there).
 *
 * This program is necessary as otherwise the trick with running the dynamic
 * linker would not allow snap-confine to retain it's root powers.
 **/

/**
 * Path where the current revision of the core snap is mounted.
 **/
#define CORE_SNAP_ROOT SNAP_MOUNT_DIR "/core/current"

int main(int argc, char **argv)
{
#if defined(__x86_64)
#define ARCH_TRIPLET "x86_64-linux-gnu"
#elif defined(__i386)
#define ARCH_TRIPLET "i386-linux-gnu"
#elif defined(__aarch64__)
#define ARCH_TRIPLET "aarch64-linux-gnu"
#elif defined(__ARMEL__) && __ARM_ARCH == 7
#define ARCH_TRIPLET "arm-linux-gnueabihf"
#elif defined(__PPC64__)
#define ARCH_TRIPLET "powerpc64le-linux-gnu"
#else
#error "where is the dynamic linker in the core snap for this architecture?"
#endif

	// Some of the paths to ld-so may contain symbolic links that use absolute
	// paths. This makes sense in a root filesystem but in an unknown
	// environment we just want to avoid them by having a good path to each
	// dynamic linker used by the (few) core snaps (one per architecture) that
	// are supported.
	const char *ld_so_path =
	    CORE_SNAP_ROOT "/lib/" ARCH_TRIPLET "/ld-2.23.so";

	// Argument array for the dynamic linker.
	// NOTE: argc + 4 and not argc + 5 because we don't need to copy argv[0].
	char *ld_so_argv[argc + 4];
	ld_so_argv[0] = "ld-2.23.so";
	// Use those libraries please.
	ld_so_argv[1] = "--library-path";
	ld_so_argv[2] = (CORE_SNAP_ROOT "/lib/" ARCH_TRIPLET ":"
			 CORE_SNAP_ROOT "/usr/lib" ARCH_TRIPLET);
	// Run snap-confine please.
	ld_so_argv[3] = CORE_SNAP_ROOT "/usr/lib/snapd/snap-confine";
	// Along with any arguments that we got.
	for (int i = 1; i < argc; ++i) {
		ld_so_argv[3 + i] = argv[i];
	}
	// Terminate the list of arguments.
	ld_so_argv[3 + argc] = NULL;
	// Run it
	execv(ld_so_path, ld_so_argv);
	perror("execv failed");
	return 1;
}
