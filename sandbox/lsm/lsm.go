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
	"fmt"
)

// ID wraps the LSM ID defined in kernel headers.
type ID uint

func (id ID) String() string {
	switch id {
	case LSM_ID_UNDEF:
		return "undef"
	case LSM_ID_CAPABILITY:
		return "capability"
	case LSM_ID_SELINUX:
		return "selinux"
	case LSM_ID_SMACK:
		return "smack"
	case LSM_ID_TOMOYO:
		return "tomoyo"
	case LSM_ID_APPARMOR:
		return "apparmor"
	case LSM_ID_YAMA:
		return "yama"
	case LSM_ID_LOADPIN:
		return "loadpin"
	case LSM_ID_SAFESETID:
		return "safesetid"
	case LSM_ID_LOCKDOWN:
		return "lockdown"
	case LSM_ID_BPF:
		return "bpf"
	case LSM_ID_LANDLOCK:
		return "landlock"
	case LSM_ID_IMA:
		return "ima"
	case LSM_ID_EVM:
		return "evm"
	case LSM_ID_IPE:
		return "ipe"
	default:
		return fmt.Sprintf("(lsm-id:%d)", id)
	}
}

// HasStringContext returns true when a given LSM is known to use strings as
// context labels.
func (id ID) HasStringContext() bool {
	switch id {
	case LSM_ID_APPARMOR, LSM_ID_SELINUX:
		return true
	default:
		return false
	}
}

// Attr wraps the kernel's attr ID for LSM queries.
type Attr uint

const (
	// https://elixir.bootlin.com/linux/v6.14/source/include/uapi/linux/lsm.h#L44
	LSM_ID_UNDEF      ID = 0
	LSM_ID_CAPABILITY ID = 100
	LSM_ID_SELINUX    ID = 101
	LSM_ID_SMACK      ID = 102
	LSM_ID_TOMOYO     ID = 103
	LSM_ID_APPARMOR   ID = 104
	LSM_ID_YAMA       ID = 105
	LSM_ID_LOADPIN    ID = 106
	LSM_ID_SAFESETID  ID = 107
	LSM_ID_LOCKDOWN   ID = 108
	LSM_ID_BPF        ID = 109
	LSM_ID_LANDLOCK   ID = 110
	LSM_ID_IMA        ID = 111
	LSM_ID_EVM        ID = 112
	LSM_ID_IPE        ID = 113

	// https://elixir.bootlin.com/linux/v6.14/source/include/uapi/linux/lsm.h#L70
	LSM_ATTR_UNDEF      Attr = 0
	LSM_ATTR_CURRENT    Attr = 100
	LSM_ATTR_EXEC       Attr = 101
	LSM_ATTR_FSCREATE   Attr = 102
	LSM_ATTR_KEYCREATE  Attr = 103
	LSM_ATTR_PREV       Attr = 104
	LSM_ATTR_SOCKCREATE Attr = 105
)

// List returns a list of currently active LSMs.
func List() ([]ID, error) {
	lsms, err := lsmListModules()
	if err != nil {
		return nil, err
	}

	repack := make([]ID, len(lsms))
	for i := 0; i < len(lsms); i++ {
		repack[i] = ID(lsms[i])
	}

	return repack, nil
}

type ContextEntry struct {
	// LSMID is the ID of the LSM owning this context information.
	LsmID ID
	// Context associated with a given LSM, can be a binary or string, depending
	// on the LSM. See [ID.HasStringContext]. When context is a string, it
	// incldues a trailing 0.
	Context []byte
}

// CurrentContext returns value of the 'current' security attribute of the
// running process, which may contain a number of context entries.
func CurrentContext() ([]ContextEntry, error) {
	return lsmGetSelfAttr(LSM_ATTR_CURRENT)
}

// ContextAsString coerces the context into a string. Only meaningful if a given
// context label is known to be a string (typically when [ID.HasStringContext()]
// is true).
func ContextAsString(ctx []byte) string {
	return string(bytes.TrimSuffix(ctx, []byte{0}))
}
