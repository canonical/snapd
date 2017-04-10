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

#include "rsa_generate_key.h"

#include <openssl/bio.h>
#include <openssl/pem.h>
#include <openssl/rand.h>
#include <openssl/rsa.h>

#include <stdint.h>
#include <stdio.h>
#include <string.h>

SnapdRSAKeyGenerationResult snapd_rsa_generate_key(uint64_t bits, SnapdRSAKeyGenerationBuffer *private_key) {
    SnapdRSAKeyGenerationResult result = SNAPD_RSA_KEY_GENERATION_SUCCESS;

    BIGNUM *bne = NULL;
    RSA *rsa = NULL;
    BIO *bp_public = NULL;
    BIO *bp_private = NULL;

    const EVP_CIPHER *empty_cipher = NULL;
    unsigned char *empty_passphrase = NULL;
    const size_t empty_passphrase_len = 0;

    private_key->memory = NULL;
    private_key->size = 0;

    if (RAND_status() != 1) {
        result = SNAPD_RSA_KEY_GENERATION_SEED_FAILURE;
        goto free_all;
    }

    bne = BN_new();
    if (bne == NULL) {
        result = SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    if (BN_set_word(bne, RSA_F4) != 1) {
        result = SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    rsa = RSA_new();
    if (rsa == NULL) {
        result = SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    if (RSA_generate_key_ex(rsa, bits, bne, NULL) != 1) {
        result = SNAPD_RSA_KEY_GENERATION_KEY_GENERATION_FAILURE;
        goto free_all;
    }

    bp_private = BIO_new(BIO_s_mem());
    if (bp_private == NULL) {
        result = SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    BIO_set_mem_buf(bp_private, BUF_MEM_new(), BIO_CLOSE);

    if (PEM_write_bio_RSAPrivateKey(bp_private, rsa, empty_cipher, empty_passphrase, empty_passphrase_len, NULL, NULL) != 1) {
        result = SNAPD_RSA_KEY_GENERATION_MARSHAL_FAILURE;
        goto free_all;
    }

free_all:
    RSA_free(rsa);
    BN_free(bne);

    if (result == SNAPD_RSA_KEY_GENERATION_SUCCESS) {
        // We are taking a copy here as we need to pass out ownership of the memory buffer.
        // The underlying memory available from buf_mem might have been allocated by SSL-specific
        // allocation functions and just free'ing it might not be correct.
        BUF_MEM *buf_mem = NULL;
        BIO_get_mem_ptr(bp_private, &buf_mem);
        private_key->memory = malloc(buf_mem->length);
        if (private_key->memory == NULL) {
            result = SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        } else {
            private_key->size = buf_mem->length;
            memcpy(private_key->memory, buf_mem->data, buf_mem->length);
        }
    }

    BIO_free_all(bp_private);

    return result;
}
