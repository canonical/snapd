// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package boot

import (
	"fmt"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

type bootState16 struct {
	varSuffix string
	errName   string
}

func newBootState16(typ snap.Type) *bootState16 {
	var varSuffix, errName string
	switch typ {
	case snap.TypeKernel:
		varSuffix = "kernel"
		errName = "kernel"
	case snap.TypeBase:
		varSuffix = "core"
		errName = "boot base"
	default:
		panic(fmt.Sprintf("cannot make a bootState16 for snap type %q", typ))
	}
	return &bootState16{varSuffix: varSuffix, errName: errName}
}

func (s16 *bootState16) revisions() (snap, try_snap *NameAndRevision, trying bool, err error) {
	bloader, err := bootloader.Find("", nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get boot settings: %s", err)
	}

	snapVar := "snap_" + s16.varSuffix
	trySnapVar := "snap_try_" + s16.varSuffix
	vars := []string{"snap_mode", snapVar, trySnapVar}
	snaps := make(map[string]*NameAndRevision, 2)

	m, err := bloader.GetBootVars(vars...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get boot variables: %s", err)
	}

	for _, vName := range vars {
		v := m[vName]
		if v == "" && vName != snapVar {
			// snap_mode & snap_try_<type> can be empty
			// snap_<type> cannot be! and will fail parsing
			// below
			continue
		}

		if vName == "snap_mode" {
			trying = v == "trying"
		} else {
			nameAndRevno, err := nameAndRevnoFromSnap(v)
			if err != nil {
				return nil, nil, false, fmt.Errorf("cannot get name and revision of %s (%s): %v", s16.errName, vName, err)
			}
			snaps[vName] = nameAndRevno
		}
	}

	return snaps[snapVar], snaps[trySnapVar], trying, nil
}

type bootStateUpdate16 struct {
	bl       bootloader.Bootloader
	env      map[string]string
	toCommit map[string]string
}

func newBootStateUpdate16(u bootStateUpdate, names ...string) (*bootStateUpdate16, error) {
	if u != nil {
		u16, ok := u.(*bootStateUpdate16)
		if !ok {
			return nil, fmt.Errorf("internal error: threading unexpected boot state update: %T", u)
		}
		return u16, nil
	}
	bl, err := bootloader.Find("", nil)
	if err != nil {
		return nil, err
	}
	m, err := bl.GetBootVars(names...)
	if err != nil {
		return nil, err
	}
	return &bootStateUpdate16{bl: bl, env: m, toCommit: make(map[string]string)}, nil
}

func (u16 *bootStateUpdate16) commit() error {
	if len(u16.toCommit) == 0 {
		// nothing to do
		return nil
	}
	env := u16.env
	// TODO: we could just SetBootVars(toCommit) but it's not
	// fully backward compatible with the preexisting behavior
	for k, v := range u16.toCommit {
		env[k] = v
	}
	return u16.bl.SetBootVars(env)
}

func (s16 *bootState16) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	u16, err := newBootStateUpdate16(update, "snap_mode", "snap_try_core", "snap_try_kernel")
	if err != nil {
		return nil, err
	}

	env := u16.env
	toCommit := u16.toCommit

	// snap_mode goes from "" -> "try" -> "trying" -> ""
	// so if we are not in "trying" mode, nothing to do here
	if env["snap_mode"] != "trying" {
		return u16, nil
	}

	tryBootVar := fmt.Sprintf("snap_try_%s", s16.varSuffix)
	bootVar := fmt.Sprintf("snap_%s", s16.varSuffix)
	// update the boot vars
	if env[tryBootVar] != "" {
		toCommit[bootVar] = env[tryBootVar]
		toCommit[tryBootVar] = ""
	}
	toCommit["snap_mode"] = ""

	return u16, nil
}

func (s16 *bootState16) setNext(nextBoot string) (rebootRequired bool, u bootStateUpdate, err error) {
	nextBootVar := fmt.Sprintf("snap_try_%s", s16.varSuffix)
	goodBootVar := fmt.Sprintf("snap_%s", s16.varSuffix)

	u16, err := newBootStateUpdate16(nil, "snap_mode", goodBootVar)
	if err != nil {
		return false, nil, err
	}

	env := u16.env
	toCommit := u16.toCommit

	snapMode := "try"
	rebootRequired = true
	if env[goodBootVar] == nextBoot {
		// If we were in anything but default ("") mode before
		// and now switch to the good core/kernel again, make
		// sure to clean the snap_mode here. This also
		// mitigates https://forum.snapcraft.io/t/5253
		if env["snap_mode"] == "" {
			// already clean
			return false, nil, nil
		}
		// clean
		snapMode = ""
		nextBoot = ""
		rebootRequired = false
	}

	toCommit["snap_mode"] = snapMode
	toCommit[nextBootVar] = nextBoot

	return rebootRequired, u16, nil
}
