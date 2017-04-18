// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package interfaces

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

type snapYaml struct {
	Name     string                 `yaml:"name"`
	RawPlugs map[string]interface{} `yaml:"plugs,omitempty"`
	RawSlots map[string]interface{} `yaml:"slots,omitempty"`
	RawApps  map[string]appYaml     `yaml:"apps,omitempty"`
}

type appYaml struct {
	SlotNames []string `yaml:"slots,omitempty"`
	PlugNames []string `yaml:"plugs,omitempty"`
}

// PlugsAndSlotsFromYaml parses the parts of snap.yaml relevant to interfaces
// and returns plugs and slots found.
func PlugsAndSlotsFromYaml(in []byte) ([]Plug, []Slot, error) {
	var y snapYaml
	if err := yaml.Unmarshal(in, &y); err != nil {
		return nil, nil, err
	}
	// Collect top-level definitions of plugs
	plugMap := make(map[string]*Plug)
	for plugName, plugData := range y.RawPlugs {
		iface, attrs, err := convertToSlotOrPlugData("plug", plugName, plugData)
		if err != nil {
			return nil, nil, err
		}
		plugMap[plugName] = &Plug{
			Snap:      y.Name,
			Name:      plugName,
			Interface: iface,
			Attrs:     attrs,
		}
	}
	// Collect top-level definitions of slots
	slotMap := make(map[string]*Slot)
	for slotName, slotData := range y.RawSlots {
		iface, attrs, err := convertToSlotOrPlugData("slot", slotName, slotData)
		if err != nil {
			return nil, nil, err
		}
		slotMap[slotName] = &Slot{
			Snap:      y.Name,
			Name:      slotName,
			Interface: iface,
			Attrs:     attrs,
		}
	}
	// Collect app-level implicit definitions of plugs and slots
	for _, app := range y.RawApps {
		for _, plugName := range app.PlugNames {
			if _, ok := plugMap[plugName]; !ok {
				plugMap[plugName] = &Plug{
					Snap:      y.Name,
					Name:      plugName,
					Interface: plugName,
				}
			}
		}
		for _, slotName := range app.SlotNames {
			if _, ok := slotMap[slotName]; !ok {
				slotMap[slotName] = &Slot{
					Snap:      y.Name,
					Name:      slotName,
					Interface: slotName,
				}
			}
		}
	}
	// Bind apps to plugs and slots
	for appName, app := range y.RawApps {
		for _, slotName := range app.SlotNames {
			slot := slotMap[slotName]
			slot.Apps = append(slot.Apps, appName)
		}
		for _, plugName := range app.PlugNames {
			plug := plugMap[plugName]
			plug.Apps = append(plug.Apps, appName)
		}
	}
	// Bind unbound plugs and slots to all apps
	for _, plug := range plugMap {
		if len(plug.Apps) == 0 {
			for appName := range y.RawApps {
				plug.Apps = append(plug.Apps, appName)
			}
		}
	}
	for _, slot := range slotMap {
		if len(slot.Apps) == 0 {
			for appName := range y.RawApps {
				slot.Apps = append(slot.Apps, appName)
			}
		}
	}
	// Flatten maps and return
	var slots []Slot
	for _, slot := range slotMap {
		slots = append(slots, *slot)
	}
	var plugs []Plug
	for _, plug := range plugMap {
		plugs = append(plugs, *plug)
	}
	return plugs, slots, nil
}

func convertToSlotOrPlugData(plugOrSlot, name string, data interface{}) (string, map[string]interface{}, error) {
	switch data.(type) {
	case map[interface{}]interface{}:
		attrs := make(map[string]interface{})
		iface := ""
		for keyData, valueData := range data.(map[interface{}]interface{}) {
			key, ok := keyData.(string)
			if !ok {
				return "", nil, fmt.Errorf("%s %q has attribute that is not a string (found %T)",
					plugOrSlot, name, keyData)
			}
			if strings.HasPrefix(key, "$") {
				return "", nil, fmt.Errorf("%s %q uses reserved attribute %q", plugOrSlot, name, key)
			}
			// XXX: perhaps we could special-case "label" the same way?
			if key == "interface" {
				value, ok := valueData.(string)
				if !ok {
					return "", nil, fmt.Errorf("interface name on %s %q is not a string (found %T)",
						plugOrSlot, name, valueData)
				}
				iface = value
			} else {
				attrs[key] = valueData
			}
		}
		if len(attrs) == 0 {
			attrs = nil
		}
		if iface == "" {
			return "", nil, fmt.Errorf("%s %q doesn't define interface name", plugOrSlot, name)
		}
		return iface, attrs, nil
	case string:
		return data.(string), nil, nil
	case nil:
		return name, nil, nil
	default:
		return "", nil, fmt.Errorf("%s %q has malformed definition (found %T)", plugOrSlot, name, data)
	}
}
