/* SPDX-License-Identifier: BSD-2-Clause
 *
 * Copyright 2023, Canonical Ltd.
 */

#ifndef FDE_KEY_HANDLER_TA_TYPE_H_
#define FDE_KEY_HANDLER_TA_TYPE_H_

// fd1b2a86-3668-11eb-adc1-0242ac120002
#define FDE_KEY_HANDLER_UUID_ID {0xfd1b2a86, 0x3668, 0x11eb, \
    { \
        0xad, 0xc1, 0x02, 0x42, 0xac, 0x12, 0x00, 0x02 \
    } \
}

#define MAX_BUF_SIZE    512
#define HANDLE_SIZE     65  // (version)(1), IV(16), NONCE(32), TAG(16)

/* Define the command index in this TA */

/*
 * TA_CMD_KEY_ENCRYPT have 3 parameters
 * Encrypts passed key with derived key
 * Key handle is randomply generated at key derivation
 * - TEE_PARAM_TYPE_MEMREF_INPUT
 *    params[0].memref.buffer: plain key buffer
 *    params[0].memref.size: lenght of the buffer
 * - TEE_PARAM_TYPE_MEMREF_OUTPUT
 *    params[1].memref.buffer: returned key handle
 *    params[1].memref.size: lenght of the buffer
 * - TEE_PARAM_TYPE_MEMREF_OUTPUT
 *    params[2].memref.buffer: returned encrypted key
 *    params[2].memref.size: lenght of the buffer
 */
#define TA_CMD_KEY_ENCRYPT            1U

/*
 * TA_CMD_KEY_DECRYPT have 3 parameters
 * - TEE_PARAM_TYPE_MEMREF_INPUT
 *    params[0].memref.buffer:  encrypted key buffer
 *    params[0].memref.size: lenght of the string
 * - TEE_PARAM_TYPE_MEMREF_INPUT
 *    params[1].memref.buffer: key handle
 *    params[1].memref.size: lenght of the buffer
 * - TEE_PARAM_TYPE_MEMREF_OUTPUT
 *    params[2].memref.buffer: returned decrypted key buffer
 *    params[2].memref.size: lenght of the buffer
 */
#define TA_CMD_KEY_DECRYPT            2U

/*
 * TA_CMD_LOCK have no parameter
 * Locks TA interface for future use till next reboot
 */
#define TA_CMD_LOCK                   3U

/*
 * TA_CMD_GET_LOCK get TA lock status
 * Gets TA interface lock state
 * - TEE_PARAM_TYPE_VALUE_OUTPUT
 *    params[0].value.a: lock status (0-unlocked, 1-locked)
 */
#define TA_CMD_GET_LOCK               4U

/*
 * TA_CMD_GEN_RANDOM generate random data
 * Generates rand data of given length
 * - TEE_PARAM_TYPE_MEMREF_OUTPUT
 *    params[1].memref.buffer: buffer to be filled with random data
 *    params[1].memref.size: lenght of the buffer
 */
#define TA_CMD_GEN_RANDOM             5U

/* Define the debug flag */
#define DEBUG
#define DLOG    MSG_RAW
//#define DLOG    ta_debug

#define UNUSED(x) (void)(x)

#endif
