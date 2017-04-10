/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

#ifndef SNAPD_OVERLORD_DEVICESTATE_RSA_GENERATE_KEY_H_
#define SNAPD_OVERLORD_DEVICESTATE_RSA_GENERATE_KEY_H_

#include <stdint.h>
#include <stdlib.h>

typedef enum {
    SNAPD_RSA_KEY_GENERATION_SUCCESS,
    SNAPD_RSA_KEY_GENERATION_SEED_FAILURE,
    SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE,
    SNAPD_RSA_KEY_GENERATION_KEY_GENERATION_FAILURE,
    SNAPD_RSA_KEY_GENERATION_IO_FAILURE
} SnapdRSAKeyGenerationResult;

typedef struct {
    char *memory;
    size_t size;
} SnapdRSAKeyGenerationBuffer;

SnapdRSAKeyGenerationResult snapd_rsa_generate_key(uint64_t bits, SnapdRSAKeyGenerationBuffer *private_key);

#endif  // SNAPD_OVERLORD_DEVICESTATE_RSA_GENERATE_KEY_H_
