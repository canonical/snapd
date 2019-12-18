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

func (s *bootState16) revisions() (snap, try_snap *NameAndRevision, trying bool, err error) {
	bloader, err := bootloader.Find("", nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get boot settings: %s", err)
	}

	snapVar := "snap_" + s.varSuffix
	trySnapVar := "snap_try_" + s.varSuffix
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
				return nil, nil, false, fmt.Errorf("cannot get name and revision of %s (%s): %v", s.errName, vName, err)
			}
			snaps[vName] = nameAndRevno
		}
	}

	return snaps[snapVar], snaps[trySnapVar], trying, nil
}
