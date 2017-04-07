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

#ifndef OVERLORD_DEVICESTATE_RSA_GENERATE_KEY_H_
#define OVERLORD_DEVICESTATE_RSA_GENERATE_KEY_H_

#include <stdint.h>

typedef enum {
    RSA_KEY_GENERATION_SUCCESS,
    RSA_KEY_GENERATION_SEED_FAILURE,
    RSA_KEY_GENERATION_ALLOCATION_FAILURE,
    RSA_KEY_GENERATION_KEY_GENERATION_FAILURE,
    RSA_KEY_GENERATION_IO_FAILURE
} RSAKeyGenerationResult;

RSAKeyGenerationResult rsa_generate_key(uint64_t bits, const char *private_key_file, const char *public_key_file);

#endif  // OVERLORD_DEVICESTATE_RSA_GENERATE_KEY_H_
