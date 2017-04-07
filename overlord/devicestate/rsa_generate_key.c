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

#include <openssl/pem.h>
#include <openssl/rand.h>
#include <openssl/rsa.h>

#include <stdint.h>
#include <stdio.h>

RSAKeyGenerationResult rsa_generate_key(uint64_t bits, char *private_key_file, char *public_key_file) {
  RSAKeyGenerationResult result = RSA_KEY_GENERATION_SUCCESS;

    BIGNUM *bne = NULL;
    RSA *rsa = NULL;
    BIO *bp_public = NULL;
    BIO *bp_private = NULL;

    const EVP_CIPHER *empty_cipher = NULL;
    unsigned char *empty_passphrase = NULL;
    const size_t empty_passphrase_len = 0;

    if (RAND_status() != 1) {
        result = RSA_KEY_GENERATION_SEED_FAILURE;
        goto free_all;
    }

    bne = BN_new();
    if (bne == NULL) {
        result = RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    if (BN_set_word(bne, RSA_F4) != 1) {
        result = RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    rsa = RSA_new();
    if (rsa == NULL) {
        result = RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    if (RSA_generate_key_ex(rsa, bits, bne, NULL) != 1) {
          result = RSA_KEY_GENERATION_KEY_GENERATION_FAILURE;
          goto free_all;
    }

    bp_public = BIO_new_file(public_key_file, "w+");
    if (bp_public == NULL) {
        result = RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    if (PEM_write_bio_RSAPublicKey(bp_public, rsa) != 1)  {
        result = RSA_KEY_GENERATION_IO_FAILURE;
        goto free_all;
    }

    bp_private = BIO_new_file(private_key_file, "w+");
    if (bp_private == NULL) {
        result = RSA_KEY_GENERATION_ALLOCATION_FAILURE;
        goto free_all;
    }

    if (PEM_write_bio_RSAPrivateKey(bp_private, rsa, empty_cipher, empty_passphrase, empty_passphrase_len, NULL, NULL) != 1) {
        result = RSA_KEY_GENERATION_IO_FAILURE;
        goto free_all;
    }

free_all:
    BIO_free_all(bp_public);
    BIO_free_all(bp_private);
    RSA_free(rsa);
    BN_free(bne);

    free(private_key_file);
    free(public_key_file);

    return result;
}
