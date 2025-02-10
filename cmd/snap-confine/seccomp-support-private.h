/*
 * Copyright (C) 2025 Canonical Ltd
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
#ifndef SNAP_CONFINE_SECCOMP_SUPPORT_PRIVATE_H
#define SNAP_CONFINE_SECCOMP_SUPPORT_PRIVATE_H

#include "seccomp-support-ext.h"
#include "seccomp-support.h"

#include <assert.h>
#include <stdint.h>
#include <stdio.h>

// Keep in sync with snap-seccomp/main.go
//
// Header of a seccomp.bin2 filter file in native byte order.
struct __attribute__((__packed__)) sc_seccomp_file_header {
    // header: "SC"
    char header[2];
    // version: 0x1
    uint8_t version;
    // flags
    uint8_t unrestricted;
    // unused
    uint8_t padding[4];

    // size of allow filter in byte
    uint32_t len_allow_filter;
    // size of deny filter in byte
    uint32_t len_deny_filter;
    // reserved for future use
    uint8_t reserved2[112];
};

static_assert(sizeof(struct sc_seccomp_file_header) == 128, "unexpected struct size");

// MAX_BPF_SIZE is an arbitrary limit.
#define MAX_BPF_SIZE (32 * 1024)

void sc_must_read_and_validate_header_from_file(FILE *file, const char *profile_path,
                                                struct sc_seccomp_file_header *hdr);

#endif
