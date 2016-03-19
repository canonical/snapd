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
	for _, yApp := range y.Apps {
		for _, plugName := range yApp.PlugNames {
			if _, ok := snap.Plugs[plugName]; !ok {
				snap.Plugs[plugName] = &PlugInfo{
					Snap:      snap,
					Name:      plugName,
					Interface: plugName,
					Apps:      make(map[string]*AppInfo),
				}
			}
		}
		for _, slotName := range yApp.SlotNames {
			if _, ok := snap.Slots[slotName]; !ok {
				snap.Slots[slotName] = &SlotInfo{
					Snap:      snap,
					Name:      slotName,
					Interface: slotName,
					Apps:      make(map[string]*AppInfo),
				}
			}
		}
	}
	// Collect definitions of apps
	for appName, yApp := range y.Apps {
		snap.Apps[appName] = &AppInfo{
			Snap:    snap,
			Name:    appName,
			Command: yApp.Command,
			Plugs:   make(map[string]*PlugInfo),
			Slots:   make(map[string]*SlotInfo),
		}
	}
	// Remember which plugs and slots are app-bound
	appBoundPlug := make(map[string]bool)
	appBoundSlot := make(map[string]bool)
	// Bind app-bound plugs and slots to their respective apps
	for appName, app := range snap.Apps {
		for _, plugName := range y.Apps[appName].PlugNames {
			appBoundPlug[plugName] = true
			plug := snap.Plugs[plugName]
			app.Plugs[plugName] = plug
			plug.Apps[appName] = app
		}
		for _, slotName := range y.Apps[appName].SlotNames {
			appBoundSlot[slotName] = true
			slot := snap.Slots[slotName]
			app.Slots[slotName] = slot
			slot.Apps[appName] = app
		}
	}
	// Bind global plugs and slots to all apps
	for plugName, plug := range snap.Plugs {
		if !appBoundPlug[plugName] {
			for appName, app := range snap.Apps {
				app.Plugs[plugName] = plug
				plug.Apps[appName] = app
			}
		}
	}
	for slotName, slot := range snap.Slots {
		if !appBoundPlug[slotName] {
			for appName, app := range snap.Apps {
				app.Slots[slotName] = slot
				slot.Apps[appName] = app
			}
		}
	}
	// FIXME: validation of the fields
	return snap, nil
}

func convertToSlotOrPlugData(plugOrSlot, name string, data interface{}) (iface string, attrs map[string]interface{}, err error) {
	iface = name
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
		return iface, attrs, nil
	case string:
		return data.(string), nil, nil
	case nil:
		return name, nil, nil
	default:
		return "", nil, fmt.Errorf("%s %q has malformed definition (found %T)", plugOrSlot, name, data)
	}
}
