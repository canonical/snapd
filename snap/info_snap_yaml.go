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
	Plugs       map[string]interface{} `yaml:"plugs,omitempty"`
	Slots       map[string]interface{} `yaml:"slots,omitempty"`
	Apps        map[string]appYaml     `yaml:"apps,omitempty"`
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
	Command   string   `yaml:"command"`
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
	for name, data := range y.Plugs {
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
	for name, data := range y.Slots {
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
	// Collect app-level implicit definitions of plugs and slots
	for _, app := range y.Apps {
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
	// Collect definitions of apps
	for name, app := range y.Apps {
		snap.Apps[name] = &AppInfo{
			Snap:    snap,
			Name:    name,
			Command: app.Command,
			Plugs:   make(map[string]*PlugInfo),
			Slots:   make(map[string]*SlotInfo),
		}
	}
	// Bind apps to plugs and slots
	for appName, app := range snap.Apps {
		if len(y.Apps[appName].PlugNames) > 0 {
			// Bind only plugs explicitly listed in this app
			for _, plugName := range y.Apps[appName].PlugNames {
				plug := snap.Plugs[plugName]
				app.Plugs[plugName] = plug
				plug.Apps[appName] = app
			}
		} else {
			// Bind all plugs defined at the top level
			for plugName := range y.Plugs {
				plug := snap.Plugs[plugName]
				app.Plugs[plugName] = plug
				plug.Apps[appName] = app
			}
		}
		if len(y.Apps[appName].SlotNames) > 0 {
			// Bind only slots explicitly listed in this app
			for _, slotName := range y.Apps[appName].SlotNames {
				slot := snap.Slots[slotName]
				app.Slots[slotName] = slot
				slot.Apps[appName] = app
			}
		} else {
			// Bind all slots defined at the top level
			for slotName := range y.Slots {
				slot := snap.Slots[slotName]
				app.Slots[slotName] = slot
				slot.Apps[appName] = app
			}
		}
	}
	// FIXME: validation of the fields
	return snap, nil
}

func convertToSlotOrPlugData(plugOrSlot, name string, data interface{}) (iface string, attrs map[string]interface{}, err error) {
	switch data.(type) {
	case map[interface{}]interface{}:
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
				if attrs == nil {
					attrs = make(map[string]interface{})
				}
				attrs[key] = valueData
			}
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
