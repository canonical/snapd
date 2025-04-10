//go:build linux && (arm || arm64)

package optee

/*
#cgo CFLAGS: -I./ta
#cgo LDFLAGS: -lteec
#include <tee_client_api.h>
#include <fde_key_handler_ta_type.h>
#include <stdint.h>

TEEC_UUID fde_ta_uuid = FDE_KEY_HANDLER_UUID_ID;

TEEC_UUID pta_device_uuid = { 0x7011a688, 0xddde, 0x4053, \
	{ \
		0xa5, 0xa9, 0x7b, 0x3c, 0x4d, 0xdf, 0x13, 0xb8 \
	} \
};
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

type opteeClient struct{}

func (c *opteeClient) FDETAPresent() bool {
	pinner := &runtime.Pinner{}
	defer pinner.Unpin()

	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: teecParamTypes(C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE),
	}
	pinner.Pin(&op)

	// this size is arbitrary, unsure what we should use here
	output := make([]byte, 1024)
	pinner.Pin(&output[0])

	outputMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[0])
	outputMemRef.size = C.size_t(len(output))
	outputMemRef.buffer = unsafe.Pointer(&output[0])

	// this PTA (psuedo trusted application) command returns a list of PTA and
	// early TA UUIDs. we know that our TA will always be an early TA.
	err := invoke(pinner, C.pta_device_uuid, 0x0 /* PTA_CMD_GET_DEVICES */, op)
	if err != nil {
		return false
	}

	// output here is a slice of bytes, each 16 byte segment is a UUID
	output = output[:outputMemRef.size]

	for i := 0; i+16 < len(output)+1; i += 16 {
		uuid, err := uuidFromOctets(output[i : i+16])
		if err != nil {
			return false
		}

		if uuid == C.fde_ta_uuid {
			return true
		}
	}

	return false
}

func (c *opteeClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	pinner := &runtime.Pinner{}
	defer pinner.Unpin()

	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: teecParamTypes(C.TEEC_MEMREF_TEMP_INPUT, C.TEEC_MEMREF_TEMP_INPUT, C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE),
	}
	pinner.Pin(op)

	pinner.Pin(&input[0])

	inputMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[0])
	inputMemRef.size = C.size_t(len(input))
	inputMemRef.buffer = unsafe.Pointer(&input[0])

	pinner.Pin(&handle[0])

	handleMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[1])
	handleMemRef.size = C.size_t(len(handle))
	handleMemRef.buffer = unsafe.Pointer(&handle[0])

	unsealed := make([]byte, C.MAX_BUF_SIZE)
	pinner.Pin(&unsealed[0])

	unsealedMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[2])
	unsealedMemRef.size = C.size_t(len(unsealed))
	unsealedMemRef.buffer = unsafe.Pointer(&unsealed[0])

	err := invoke(pinner, C.fde_ta_uuid, C.TA_CMD_KEY_DECRYPT, op)
	if err != nil {
		return nil, err
	}

	unsealed = unsealed[:unsealedMemRef.size]

	return unsealed, nil
}

func (c *opteeClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	pinner := &runtime.Pinner{}
	defer pinner.Unpin()

	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: teecParamTypes(C.TEEC_MEMREF_TEMP_INPUT, C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_MEMREF_TEMP_OUTPUT, C.TEEC_NONE),
	}
	pinner.Pin(op)

	pinner.Pin(&input[0])

	inputMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[0])
	inputMemRef.size = C.size_t(len(input))
	inputMemRef.buffer = unsafe.Pointer(&input[0])

	handle = make([]byte, C.HANDLE_SIZE)
	pinner.Pin(&handle[0])

	handleMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[1])
	handleMemRef.size = C.size_t(len(handle))
	handleMemRef.buffer = unsafe.Pointer(&handle[0])

	sealed = make([]byte, C.MAX_BUF_SIZE)
	pinner.Pin(&sealed[0])

	sealedMemRef := unionAsType[C.TEEC_TempMemoryReference](&op.params[2])
	sealedMemRef.size = C.size_t(len(sealed))
	sealedMemRef.buffer = unsafe.Pointer(&sealed[0])

	err = invoke(pinner, C.fde_ta_uuid, C.TA_CMD_KEY_ENCRYPT, op)
	if err != nil {
		return nil, nil, err
	}

	handle = handle[:handleMemRef.size]
	sealed = sealed[:sealedMemRef.size]

	return handle, sealed, nil
}

func (c *opteeClient) LockTA() error {
	pinner := &runtime.Pinner{}
	defer pinner.Unpin()

	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: teecParamTypes(C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE),
	}
	pinner.Pin(op)

	return invoke(pinner, C.fde_ta_uuid, C.TA_CMD_LOCK, op)
}

func (c *opteeClient) Version() (string, error) {
	return "", errors.New("TODO")
}

func newOPTEEClient() Client {
	return &opteeClient{}
}

func uuidFromOctets(s []byte) (C.TEEC_UUID, error) {
	if len(s) != 16 {
		return C.TEEC_UUID{}, fmt.Errorf("cannot parse slice as uuid: length is %d, expected 16", len(s))
	}

	d := C.TEEC_UUID{
		timeLow:          C.uint32_t(binary.BigEndian.Uint32(s[0:4])),
		timeMid:          C.uint16_t(binary.BigEndian.Uint16(s[4:6])),
		timeHiAndVersion: C.uint16_t(binary.BigEndian.Uint16(s[6:8])),
	}
	for i, b := range s[8:] {
		d.clockSeqAndNode[i] = C.uint8_t(b)
	}

	return d, nil
}

// unionAsType interprets the memory that union points to as a T. This is useful
// when working with C unions, since they are converted to byte arrays when used
// from Go.
func unionAsType[T any, U any](union *U) *T {
	return (*T)(unsafe.Pointer(union))
}

// teecParamTypes is a Go version of TEEC_PARAM_TYPES, since that is a macro and
// cannot be used from Go.
func teecParamTypes(p0, p1, p2, p3 C.uint32_t) C.uint32_t {
	return p0 | (p1 << 4) | (p2 << 8) | (p3 << 12)
}

func invoke(pinner *runtime.Pinner, uuid C.TEEC_UUID, cmd uint32, op *C.TEEC_Operation) error {
	var ctx C.TEEC_Context
	pinner.Pin(&ctx)

	res := C.TEEC_InitializeContext(nil, &ctx)
	if res != 0 {
		return fmt.Errorf("cannot initalize op-tee context: 0x%x", uint32(res))
	}
	defer C.TEEC_FinalizeContext(&ctx)

	var code C.uint32_t
	var sess C.TEEC_Session
	pinner.Pin(&code)
	pinner.Pin(&sess)
	pinner.Pin(&uuid)

	res = C.TEEC_OpenSession(&ctx, &sess, &uuid, C.TEEC_LOGIN_PUBLIC, nil, nil, &code)
	if res != 0 {
		return fmt.Errorf("cannot open op-tee session: 0x%x", uint32(res))
	}
	defer C.TEEC_CloseSession(&sess)

	code = 0

	res = C.TEEC_InvokeCommand(&sess, C.uint32_t(cmd), op, &code)
	if res != C.TEEC_SUCCESS {
		return fmt.Errorf("cannot invoke op-tee command: 0x%x", uint32(res))
	}

	return nil
}
