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

// Snap20 carries options for a model snap or an extra snap
// in grade: dangerous.
type Snap20 struct {
	Name string `yaml:"name"`
	// id and unasserted can be both set, in which case it only
	// cross-references the model
	SnapID string `yaml:"id,omitempty"`

	// Unasserted has the filename for an unasserted local snap
	Unasserted string `yaml:"unasserted,omitempty"`

	Channel string `yaml:"channel,omitempty"`
	// TODO: DevMode bool   `yaml:"devmode,omitempty"`
}

// SnapName implements naming.SnapRef.
func (sn *Snap20) SnapName() string {
	return sn.Name
}

// ID implements naming.SnapRef.
func (sn *Snap20) ID() string {
	return sn.SnapID
}

type Options20 struct {
	Snaps []*Snap20 `yaml:"snaps"`
}

func ReadOptions20(optionsFn string) (*Options20, error) {
	errPrefix := "cannot read grade dangerous options yaml"

	yamlData := mylog.Check2(os.ReadFile(optionsFn))

	var options Options20
	mylog.Check(yaml.Unmarshal(yamlData, &options))

	seenNames := make(map[string]bool, len(options.Snaps))
	// validate
	for _, sn := range options.Snaps {
		if sn == nil {
			return nil, fmt.Errorf("%s: empty snaps element", errPrefix)
		}
		mylog.Check(
			// TODO: check if it's a parallel install explicitly,
			// need to move *Instance* helpers from snap to naming
			naming.ValidateSnap(sn.Name))

		if sn.SnapID == "" && sn.Channel == "" && sn.Unasserted == "" {
			return nil, fmt.Errorf("%s: at least one of id, channel or unasserted must be set for snap %q", errPrefix, sn.Name)
		}
		if sn.SnapID != "" {
			mylog.Check(naming.ValidateSnapID(sn.SnapID))
		}
		if sn.Channel != "" {
			mylog.Check2(channel.Parse(sn.Channel, ""))
		}
		if sn.Unasserted != "" && strings.Contains(sn.Unasserted, "/") {
			return nil, fmt.Errorf("%s: %q must be a filename, not a path", errPrefix, sn.Unasserted)
		}

		// make sure names and file names are unique
		if seenNames[sn.Name] {
			return nil, fmt.Errorf("%s: snap name %q must be unique", errPrefix, sn.Name)
		}
		seenNames[sn.Name] = true
	}

	return &options, nil
}

func (options *Options20) Write(optionsFn string) error {
	data := mylog.Check2(yaml.Marshal(options))
	mylog.Check(osutil.AtomicWriteFile(optionsFn, data, 0644, 0))

	return nil
}
