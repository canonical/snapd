// -*- Mode: Go; indent-tabs-mode: t -*-

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

package lsm

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
)

func lsmListModules() ([]uint64, error) {
	// see
	// https://elixir.bootlin.com/linux/v6.14/source/security/lsm_syscalls.c#L84 for syscall documentation

	var bufsz uint32

	// find out how much space we need
	_, _, errno := syscall.Syscall(unix.SYS_LSM_LIST_MODULES,
		uintptr(0), uintptr(unsafe.Pointer(&bufsz)), 0)
	if errno != syscall.E2BIG {
		// could be ENOSYS
		return nil, errno
	}

	// bufsz is the size of a contiguous buffer we need to hold a list of all active LSMs
	count := uintptr(bufsz) / unsafe.Sizeof(uint64(0))
	// the buffer itself
	buf := make([]uint64, count)

	r1, _, errno := syscall.Syscall(unix.SYS_LSM_LIST_MODULES,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bufsz)), 0)
	if errno != 0 {
		return nil, errno
	}

	return buf[:r1], nil
}

func lsmGetSelfAttr(attr Attr) ([]ContextEntry, error) {
	// https://elixir.bootlin.com/linux/v6.14/source/include/uapi/linux/lsm.h#L17
	// struct lsm_ctx {
	//     __u64 id;
	//     __u64 flags;
	//     __u64 len;
	//     __u64 ctx_len;
	//     __u8 ctx[] __counted_by(ctx_len);
	// };
	type kernelLSMCtx struct {
		ID    uint64
		Flags uint64
		// total length including this struct, context, any other data and
		// padding
		Len uint64
		// context field length
		CtxLen uint64
	}

	// find out how much space we need
	var sz uint32 = 0
	_, _, errno := syscall.Syscall6(unix.SYS_LSM_GET_SELF_ATTR,
		uintptr(attr),
		uintptr(0), uintptr(unsafe.Pointer(&sz)),
		uintptr(0), uintptr(0), uintptr(0))
	if errno != syscall.E2BIG {
		// could be ENOSYS
		return nil, errno
	}

	buf := make([]byte, sz)
	// returns the count of context entries
	count, _, errno := syscall.Syscall6(unix.SYS_LSM_GET_SELF_ATTR,
		uintptr(attr),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)),
		uintptr(0), uintptr(0), uintptr(0))
	if errno != 0 {
		return nil, errno
	}

	entries := make([]ContextEntry, 0, count)
	// TODO use binary.NativeEndian
	endian := arch.Endian()

	for i := 0; i < int(count); i++ {
		var lsmAttrData kernelLSMCtx

		// TODO use binary.Decode()
		n := unsafe.Sizeof(lsmAttrData)
		if err := binary.Read(bytes.NewReader(buf[:n]), endian, &lsmAttrData); err != nil {
			return nil, err
		}

		entries = append(entries, ContextEntry{
			LsmID:   ID(lsmAttrData.ID),
			Context: buf[n : uint64(n)+lsmAttrData.CtxLen],
		})

		buf = buf[lsmAttrData.Len:]
	}

	return entries, nil
}
