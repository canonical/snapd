// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package seed

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
)

// ValidateFromYaml validates the given seed.yaml file and surrounding seed.
func ValidateFromYaml(seedYamlFile string) error {
	seed, err := internal.ReadSeedYaml(seedYamlFile)
	if err != nil {
		return err
	}

	var errs []error
	// read the snaps info
	snapInfos := make(map[string]*snap.Info)
	for _, seedSnap := range seed.Snaps {
		fn := filepath.Join(filepath.Dir(seedYamlFile), "snaps", seedSnap.File)
		snapf, err := snap.Open(fn)
		if err != nil {
			errs = append(errs, err)
		} else {
			info, err := snap.ReadInfoFromSnapFile(snapf, nil)
			if err != nil {
				errs = append(errs, fmt.Errorf("cannot use snap %s: %v", fn, err))
			} else {
				snapInfos[info.InstanceName()] = info
			}
		}
	}

	// ensure we have either "core" or "snapd"
	_, haveCore := snapInfos["core"]
	_, haveSnapd := snapInfos["snapd"]
	if !(haveCore || haveSnapd) {
		errs = append(errs, fmt.Errorf("the core or snapd snap must be part of the seed"))
	}

	if errs2 := snap.ValidateBasesAndProviders(snapInfos); errs2 != nil {
		errs = append(errs, errs2...)
	}
	if errs != nil {
		var buf bytes.Buffer
		for _, err := range errs {
			fmt.Fprintf(&buf, "\n- %s", err)
		}
		return fmt.Errorf("cannot validate seed:%s", buf.Bytes())
	}

	return nil
}
