// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package internal

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
)

// Snap16 points to a snap in the seed to install, together with
// assertions (or alone if unasserted is true) it will be used to
// drive the installation and ultimately set SideInfo/SnapState for it.
type Snap16 struct {
	Name string `yaml:"name"`

	// cross-reference/audit
	SnapID string `yaml:"snap-id,omitempty"`

	// bits that are orthongonal/not in assertions
	Channel string `yaml:"channel,omitempty"`
	DevMode bool   `yaml:"devmode,omitempty"`
	Classic bool   `yaml:"classic,omitempty"`

	Private bool `yaml:"private,omitempty"`

	Contact string `yaml:"contact,omitempty"`

	// no assertions are available in the seed for this snap
	Unasserted bool `yaml:"unasserted,omitempty"`

	File string `yaml:"file"`
}

type Seed16 struct {
	Snaps []*Snap16 `yaml:"snaps"`
}

func ReadSeedYaml(fn string) (*Seed16, error) {
	errPrefix := "cannot read seed yaml"

	yamlData := mylog.Check2(os.ReadFile(fn))

	var seed Seed16
	mylog.Check(yaml.Unmarshal(yamlData, &seed))

	seenNames := make(map[string]bool, len(seed.Snaps))
	// validate
	for _, sn := range seed.Snaps {
		if sn == nil {
			return nil, fmt.Errorf("%s: empty element in seed", errPrefix)
		}
		mylog.Check(
			// TODO: check if it's a parallel install explicitly,
			// need to move *Instance* helpers from snap to naming
			naming.ValidateSnap(sn.Name))

		if sn.Channel != "" {
			mylog.Check2(channel.Parse(sn.Channel, ""))
		}
		if sn.File == "" {
			return nil, fmt.Errorf(`%s: "file" attribute for %q cannot be empty`, errPrefix, sn.Name)
		}
		if strings.Contains(sn.File, "/") {
			return nil, fmt.Errorf("%s: %q must be a filename, not a path", errPrefix, sn.File)
		}

		// make sure names and file names are unique
		if seenNames[sn.Name] {
			return nil, fmt.Errorf("%s: snap name %q must be unique", errPrefix, sn.Name)
		}
		seenNames[sn.Name] = true
	}

	return &seed, nil
}

func (seed *Seed16) Write(seedFn string) error {
	data := mylog.Check2(yaml.Marshal(&seed))
	mylog.Check(osutil.AtomicWriteFile(seedFn, data, 0644, 0))

	return nil
}
