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

package snap

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

type snapYaml struct {
	Name        string                 `yaml:"name"`
	Developer   string                 `yaml:"developer"`
	Version     string                 `yaml:"version"`
	Type        Type                   `yaml:"type"`
	Channel     string                 `yaml:"channel"`
	Description string                 `yaml:"description"`
	RawPlugs    map[string]interface{} `yaml:"plugs,omitempty"`
	RawSlots    map[string]interface{} `yaml:"slots,omitempty"`
	RawApps     map[string]appYaml     `yaml:"apps,omitempty"`
}

type plugYaml struct {
	Interface string                 `yaml:"interface"`
	Attrs     map[string]interface{} `yaml:"attrs,omitempty"`
	Apps      []string               `yaml:"apps,omitempty"`
	Label     string                 `yaml:"label"`
}

type slotYaml struct {
	Interface string                 `yaml:"interface"`
	Attrs     map[string]interface{} `yaml:"attrs,omitempty"`
	Apps      []string               `yaml:"apps,omitempty"`
	Label     string                 `yaml:"label"`
}

type appYaml struct {
	SlotNames []string `yaml:"slots,omitempty"`
	PlugNames []string `yaml:"plugs,omitempty"`
}

// InfoFromSnapYaml creates a new info based on the given snap.yaml data
func InfoFromSnapYaml(yamlData []byte) (*Info, error) {
	var y snapYaml
	err := yaml.Unmarshal(yamlData, &y)
	if err != nil {
		return nil, fmt.Errorf("info failed to parse: %s", err)
	}
	// Construct snap skeleton, without apps, plugs and slots
	snap := &Info{
		Name:        y.Name,
		Developer:   y.Developer,
		Version:     y.Version,
		Type:        y.Type,
		Channel:     y.Channel,
		Description: y.Description,
		Apps:        make(map[string]*AppInfo),
		Plugs:       make(map[string]*PlugInfo),
		Slots:       make(map[string]*SlotInfo),
	}
	// Collect top-level definitions of plugs
	for name, data := range y.RawPlugs {
		iface, attrs, err := convertToSlotOrPlugData("plug", name, data)
		if err != nil {
			return nil, err
		}
		snap.Plugs[name] = &PlugInfo{
			Snap:      snap,
			Name:      name,
			Interface: iface,
			Attrs:     attrs,
			Apps:      make(map[string]*AppInfo),
		}
	}
	// Collect top-level definitions of slots
	for name, data := range y.RawSlots {
		iface, attrs, err := convertToSlotOrPlugData("slot", name, data)
		if err != nil {
			return nil, err
		}
		snap.Slots[name] = &SlotInfo{
			Snap:      snap,
			Name:      name,
			Interface: iface,
			Attrs:     attrs,
			Apps:      make(map[string]*AppInfo),
		}
	}
	// Collect definitions of apps
	for name, app := range y.RawApps {
		snap.Apps[name] = &AppInfo{
			Snap:  snap,
			Name:  name,
			slots: app.SlotNames,
			plugs: app.PlugNames,
		}
	}
	// Collect app-level implicit definitions of plugs and slots
	for _, app := range y.RawApps {
		for _, name := range app.PlugNames {
			if _, ok := snap.Plugs[name]; !ok {
				snap.Plugs[name] = &PlugInfo{
					Snap:      snap,
					Name:      name,
					Interface: name,
					Apps:      make(map[string]*AppInfo),
				}
			}
		}
		for _, name := range app.SlotNames {
			if _, ok := snap.Slots[name]; !ok {
				snap.Slots[name] = &SlotInfo{
					Snap:      snap,
					Name:      name,
					Interface: name,
					Apps:      make(map[string]*AppInfo),
				}
			}
		}
	}
	// Bind apps to plugs and slots
	for appName, app := range y.RawApps {
		for _, slotName := range app.SlotNames {
			snap.Slots[slotName].Apps[appName] = snap.Apps[appName]
		}
		for _, plugName := range app.PlugNames {
			snap.Plugs[plugName].Apps[appName] = snap.Apps[appName]
		}
	}
	// Bind unbound plugs and slots to all apps
	for _, plug := range snap.Plugs {
		if len(plug.Apps) == 0 {
			for name := range y.RawApps {
				plug.Apps[name] = snap.Apps[name]
			}
		}
	}
	for _, slot := range snap.Slots {
		if len(slot.Apps) == 0 {
			for name := range y.RawApps {
				slot.Apps[name] = snap.Apps[name]
			}
		}
	}
	// Bind unbound apps to all plugs and slots
	for _, app := range snap.Apps {
		// NOTE: This used RawPlugs and RawSlots so that implicitly defined
		// (non-top-level) plugs and slots don't get bound here.
		if len(app.plugs) == 0 {
			for name := range y.RawPlugs {
				app.plugs = append(app.plugs, name)
			}
		}
		if len(app.slots) == 0 {
			for name := range y.RawSlots {
				app.slots = append(app.slots, name)
			}
		}
	}
	// FIXME: validation of the fields
	return snap, nil
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
