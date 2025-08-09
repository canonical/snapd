// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build optee && linux && (arm || arm64)

/*
 * Copyright (C) Canonical Ltd
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

package optee

/*
#cgo CFLAGS: -I./
#cgo LDFLAGS: -lteec
#include <tee_client_api.h>
#include <fde_key_handler_ta_type.h>
#include <stdint.h>
#include <stdlib.h>

TEEC_UUID fde_ta_uuid = FDE_KEY_HANDLER_UUID_ID;

TEEC_UUID pta_device_uuid = { 0x7011a688, 0xddde, 0x4053, \
	{ \
		0xa5, 0xa9, 0x7b, 0x3c, 0x4d, 0xdf, 0x13, 0xb8 \
	} \
};
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"unsafe"

	"github.com/snapcore/snapd/logger"
)

type opteeClient struct{}

func (c *opteeClient) Present() bool {
	// the first time we invoke the PTA without sending a buffer to fill. the
	// PTA handles this by just returning the size of the buffer it will need.
	bufferSize, err := devicesBufferSize()
	if err != nil {
		return false
	}

	// parameters:
	// - output parameter containing a slice of bytes, each 16 byte segment is a UUID
	// - none
	// - none
	// - none
	params := teecParamTypes(C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE)
	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: params,
	}

	outputMemRef, free := addMemRefToOp(op, 0, make([]byte, bufferSize))
	defer free()

	// this PTA (psuedo trusted application) command returns a list of PTA and
	// early TA UUIDs. we know that our TA will always be an early TA.
	if err := invoke(C.pta_device_uuid, 0x0 /* PTA_CMD_GET_DEVICES */, op); err != nil {
		return false
	}

	// output here is a slice of bytes, each 16 byte segment is a UUID
	output := C.GoBytes(outputMemRef.buffer, C.int(outputMemRef.size))

	// fdeUUID is the byte encoding of the FDE TA UUID (FDE_KEY_HANDLER_UUID_ID
	// in fde_key_handler_ta_type.h). having this pre-calculated eliminates some
	// code that would be required to do the conversion.
	fdeUUID := [...]byte{0xfd, 0x1b, 0x2a, 0x86, 0x36, 0x68, 0x11, 0xeb, 0xad, 0xc1, 0x2, 0x42, 0xac, 0x12, 0x0, 0x2}

	for i := 0; i+16 < len(output)+1; i += 16 {
		if !bytes.Equal(fdeUUID[:], output[i:i+16]) {
			continue
		}

		version, err := c.Version()
		if err != nil {
			logger.Noticef("FDE TA found")
		} else {
			logger.Noticef("FDE TA version %q found", version)
		}

		return true
	}

	return false
}

func devicesBufferSize() (int, error) {
	// parameters:
	// - output parameter containing a slice of bytes, each 16 byte segment is a
	//   UUID; in this case, we will send in an empty slice to just retrieve the
	//   required buffer size
	// - none
	// - none
	// - none
	params := teecParamTypes(C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE)
	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: params,
	}

	// we don't use addMemRefToOp here because we don't need to allocate
	// anything
	memRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[0])
	memRef.size = 0
	memRef.buffer = nil

	// the first time we invoke the PTA without sending a buffer to fill. the
	// PTA handles this by just returning the size of the buffer it will need.
	res, err := invokeUnchecked(C.pta_device_uuid, 0x0 /* PTA_CMD_GET_DEVICES */, op)
	if err != nil {
		return 0, err
	}

	if res != C.TEEC_ERROR_SHORT_BUFFER {
		return 0, fmt.Errorf("expected short buffer error from PTA: %v", res)
	}

	return int(memRef.size), nil
}

func addMemRefToOp(op *C.TEEC_Operation, index int, buffer []byte) (*C.TEEC_TempMemoryReference, func()) {
	malloced := C.CBytes(buffer)

	memRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[index])
	memRef.size = C.size_t(len(buffer))
	memRef.buffer = malloced

	return memRef, func() {
		C.free(malloced)
	}
}

func (c *opteeClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	if len(input) == 0 || len(handle) == 0 {
		return nil, errors.New("internal error: input and handle both must contain data")
	}

	// parameters:
	// - input parameter containing the encrypted key
	// - input parameter containing the handle
	// - output parameter containing the decrypted key
	// - none
	params := teecParamTypes(C.TEEC_MEMREF_TEMP_INPUT, C.TEEC_MEMREF_TEMP_INPUT, C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE)
	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: params,
	}

	_, free := addMemRefToOp(op, 0, input)
	defer free()

	_, free = addMemRefToOp(op, 1, handle)
	defer free()

	unsealedMemRef, free := addMemRefToOp(op, 2, make([]byte, C.MAX_BUF_SIZE))
	defer free()

	err := invoke(C.fde_ta_uuid, C.TA_CMD_KEY_DECRYPT, op)
	if err != nil {
		return nil, err
	}

	unsealed := C.GoBytes(unsealedMemRef.buffer, C.int(unsealedMemRef.size))

	return unsealed, nil
}

func (c *opteeClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	if len(input) == 0 {
		return nil, nil, errors.New("internal error: input must contain data")
	}

	// parameters:
	// - input parameter containing the key to encrypt
	// - output parameter containing the handle
	// - output parameter containing the encrypted key
	// - none
	params := teecParamTypes(C.TEEC_MEMREF_TEMP_INPUT, C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE)
	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: params,
	}

	_, free := addMemRefToOp(op, 0, input)
	defer free()

	handleMemRef, free := addMemRefToOp(op, 1, make([]byte, C.HANDLE_SIZE))
	defer free()

	sealedMemRef, free := addMemRefToOp(op, 2, make([]byte, C.MAX_BUF_SIZE))
	defer free()

	err = invoke(C.fde_ta_uuid, C.TA_CMD_KEY_ENCRYPT, op)
	if err != nil {
		return nil, nil, err
	}

	handle = C.GoBytes(handleMemRef.buffer, C.int(handleMemRef.size))
	sealed = C.GoBytes(sealedMemRef.buffer, C.int(sealedMemRef.size))

	return handle, sealed, nil
}

func (c *opteeClient) Lock() error {
	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: teecParamTypes(C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE),
	}

	return invoke(C.fde_ta_uuid, C.TA_CMD_LOCK, op)
}

func (c *opteeClient) Version() (int, error) {
	// parameters:
	// - output parameter containing the version
	// - none
	// - none
	// - none
	params := teecParamTypes(C.TEEC_VALUE_OUTPUT, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE)
	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: params,
	}

	value := unionAsType[C.TEEC_Value](&op.params[0])

	if err := invoke(C.fde_ta_uuid, C.TA_CMD_TA_VERSION, op); err != nil {
		return 0, err
	}

	return int(value.a), nil
}

func newFDETAClient() FDETAClient {
	return &opteeClient{}
}

// unionAsType interprets the memory that union points to as a T. This is useful
// when working with C unions, since they are converted to byte arrays when used
// from Go.
func unionAsType[T any, U any](union *U) *T {
	return (*T)(unsafe.Pointer(union))
}

// teecParamTypes is a Go version of TEEC_PARAM_TYPES, since that is a macro and
// cannot be used from Go.
//
// OPTEE TAs support a few parameter types, we use these:
//   - TEEC_MEMREF_TEMP_INPUT: input parameter containing a slice of bytes
//   - TEEC_MEMREF_TEMP_OUTPUT: output parameter containing a slice of bytes
//   - TEEC_NONE: unused parameter
func teecParamTypes(p0, p1, p2, p3 C.uint32_t) C.uint32_t {
	return p0 | (p1 << 4) | (p2 << 8) | (p3 << 12)
}

func invoke(uuid C.TEEC_UUID, cmd uint32, op *C.TEEC_Operation) error {
	res, err := invokeUnchecked(uuid, cmd, op)
	if err != nil {
		return err
	}

	if res != C.TEEC_SUCCESS {
		return fmt.Errorf("cannot invoke op-tee command: 0x%x", uint32(res))
	}
	return nil
}

func invokeUnchecked(uuid C.TEEC_UUID, cmd uint32, op *C.TEEC_Operation) (C.TEEC_Result, error) {
	// the optee library stores a pointer to the context (ctx) when opening a
	// session (sess) inside of the session struct. to avoid passing a go
	// pointer containing another go pointer into C (which cgo forbids), we must
	// allocate the context itself in C memory
	ctx := (*C.TEEC_Context)(C.malloc(C.size_t(unsafe.Sizeof(C.TEEC_Context{}))))
	if ctx == nil {
		return 0, errors.New("cannot allocate op-tee context")
	}
	defer C.free(unsafe.Pointer(ctx))

	res := C.TEEC_InitializeContext(nil, ctx)
	if res != 0 {
		return 0, fmt.Errorf("cannot initalize op-tee context: 0x%x", uint32(res))
	}
	defer C.TEEC_FinalizeContext(ctx)

	var code C.uint32_t
	var sess C.TEEC_Session

	res = C.TEEC_OpenSession(ctx, &sess, &uuid, C.TEEC_LOGIN_PUBLIC, nil, nil, &code)
	if res != 0 {
		return 0, fmt.Errorf("cannot open op-tee session: 0x%x", uint32(res))
	}
	defer C.TEEC_CloseSession(&sess)

	code = 0

	return C.TEEC_InvokeCommand(&sess, C.uint32_t(cmd), op, &code), nil
}
