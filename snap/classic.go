// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Zygmunt Krynicki
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

package snap

import (
	"errors"
	"fmt"
	"github.com/snapcore/snapd/dirs"
	"os"
	"path/filepath"

	// Note: upgrading to v3 requires deeper changes, as the output of deserializing differs.
	"gopkg.in/yaml.v2"
)

type classicYaml struct {
	Slots map[string]interface{} `yaml:"slots,omitempty"`
}

type ClassicSlotInfo struct {
	Name      string
	Label     string
	Interface string
	Attrs     map[string]interface{}
}

// LoadClassicSlots loads statically-defined slots.
//
// On classic the gadget snap is unavailable, so there is no mechanism
// to describe certain static hardware elements, like platform serial ports,
// iio, GPIO or PWM interfaces.
//
// The system administrator can describe any such slots using the classic.yaml
// configuration file located in /etc/snapd/ directory.
func LoadClassicSlots() ([]ClassicSlotInfo, error) {
	f, err := os.Open(filepath.Join(dirs.SnapEtcDir, "classic.yaml"))

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}

		return nil, err
	}

	defer func() {
		_ = f.Close()
	}()

	var y classicYaml

	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&y); err != nil {
		return nil, fmt.Errorf("cannot parse classic.yaml: %s", err)
	}

	slots := make([]ClassicSlotInfo, 0, len(y.Slots))

	for name, data := range y.Slots {
		iface, label, attrs, err := convertToSlotOrPlugData("slot", name, data)
		if err != nil {
			return nil, err
		}

		slots = append(slots, ClassicSlotInfo{
			Name:      name,
			Interface: iface,
			Attrs:     attrs,
			Label:     label,
		})
	}

	return slots, nil
}
