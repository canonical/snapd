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
	"encoding/binary"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
)

const (
	// https://elixir.bootlin.com/linux/v6.14/source/include/uapi/linux/lsm.h#L44
	LSM_ID_UNDEF      = 0
	LSM_ID_CAPABILITY = 100
	LSM_ID_SELINUX    = 101
	LSM_ID_SMACK      = 102
	LSM_ID_TOMOYO     = 103
	LSM_ID_APPARMOR   = 104
	LSM_ID_YAMA       = 105
	LSM_ID_LOADPIN    = 106
	LSM_ID_SAFESETID  = 107
	LSM_ID_LOCKDOWN   = 108
	LSM_ID_BPF        = 109
	LSM_ID_LANDLOCK   = 110
	LSM_ID_IMA        = 111
	LSM_ID_EVM        = 112
	LSM_ID_IPE        = 113

	lsmCount = LSM_ID_IPE - LSM_ID_CAPABILITY + 1

	// https://elixir.bootlin.com/linux/v6.14/source/include/uapi/linux/lsm.h#L70
	LSM_ATTR_UNDEF      = 0
	LSM_ATTR_CURRENT    = 100
	LSM_ATTR_EXEC       = 101
	LSM_ATTR_FSCREATE   = 102
	LSM_ATTR_KEYCREATE  = 103
	LSM_ATTR_PREV       = 104
	LSM_ATTR_SOCKCREATE = 105
)

// List returns a list of currently active LSMs.
func List() ([]uint64, error) {
	lsms, err := lsmListModules()
	if err != nil {
		return nil, err
	}

	return lsms, nil
}

func lsmListModules() ([]uint64, error) {
	// see
	// https://elixir.bootlin.com/linux/v6.14/source/security/lsm_syscalls.c#L84 for syscall documentation

	var bufsz uint32

	// find out how much space we need
	_, _, errno := syscall.Syscall(unix.SYS_LSM_LIST_MODULES,
		uintptr(0), uintptr(unsafe.Pointer(&bufsz)), 0)
	if errno != syscall.E2BIG {
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

type ContextEntry struct {
	// ID of the LSM
	ID uint64
	// Context associated with a given LSM, can be a binary or string, depending
	// on the LSM.
	Context []byte
}

// CurrentContext returns value of the 'current' security attribute of the
// running process, which may contain a number of context entries.
func CurrentContext() ([]ContextEntry, error) {
	return lsmGetSelfAttr(LSM_ATTR_CURRENT)
}

func lsmGetSelfAttr(attr uint) ([]ContextEntry, error) {
	// https://elixir.bootlin.com/linux/v6.14/source/include/uapi/linux/lsm.h#L17
	// struct lsm_ctx {
	//     __u64 id;
	//     __u64 flags;
	//     __u64 len;
	//     __u64 ctx_len;
	//     __u8 ctx[] __counted_by(ctx_len);
	// };
	type kernelLSMCtx struct {
		ID     uint64
		Flags  uint64
		Len    uint64
		CtxLen uint64
	}

	// find out how much space we need
	var sz uint32 = 0
	_, _, errno := syscall.Syscall6(unix.SYS_LSM_GET_SELF_ATTR,
		uintptr(attr),
		uintptr(0), uintptr(unsafe.Pointer(&sz)),
		uintptr(0), uintptr(0), uintptr(0))
	if errno != syscall.E2BIG {
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
	for i := 0; i < int(count); i++ {
		// TODO use binary.NativeEndian
		var lsmAttrData kernelLSMCtx
		endian := arch.Endian()
		n, err := binary.Decode(buf, endian, &lsmAttrData)
		if err != nil {
			return nil, err
		}

		entries = append(entries, ContextEntry{
			ID:      lsmAttrData.ID,
			Context: buf[n : uint64(n)+lsmAttrData.CtxLen],
		})

		buf = buf[lsmAttrData.Len:]
	}

	return entries, nil
}
