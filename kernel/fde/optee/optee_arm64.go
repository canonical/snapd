//go:build arm || arm64

package optee

// hacky for now, long term we want to use the optee-client deb, but that isn't
// conducive to working on this from an x86 machine.
//
//go:generate ./build_client.sh

/*
#cgo CFLAGS: -I./optee_client/install/export/usr/include -I./ta
#cgo LDFLAGS: -L./optee_client/install/libteec/ -Wl,-Bstatic -lteec -Wl,-Bdynamic
#include <tee_client_api.h>
#include <fde_key_handler_ta_type.h>
#include <stdint.h>

TEEC_UUID fde_ta_uuid = FDE_KEY_HANDLER_UUID_ID;
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

func DecryptKey(input []byte, handle []byte) ([]byte, error) {
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

	err := invoke(pinner, C.TA_CMD_KEY_DECRYPT, op)
	if err != nil {
		return nil, err
	}

	unsealed = unsealed[:unsealedMemRef.size]

	return unsealed, nil
}

func EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
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

	err = invoke(pinner, C.TA_CMD_KEY_ENCRYPT, op)
	if err != nil {
		return nil, nil, err
	}

	handle = handle[:handleMemRef.size]
	sealed = sealed[:sealedMemRef.size]

	return handle, sealed, nil
}

func LockTA() error {
	pinner := &runtime.Pinner{}
	defer pinner.Unpin()

	op := &C.TEEC_Operation{
		started:    1,
		paramTypes: teecParamTypes(C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE, C.TEEC_NONE),
	}
	pinner.Pin(op)

	return invoke(pinner, C.TA_CMD_LOCK, op)
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
	return ((p0) | ((p1) << 4) | ((p2) << 8) | ((p3) << 12))
}

func invoke(pinner *runtime.Pinner, cmd uint32, op *C.TEEC_Operation) error {
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

	res = C.TEEC_OpenSession(&ctx, &sess, &C.fde_ta_uuid, C.TEEC_LOGIN_PUBLIC, nil, nil, &code)
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
