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
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

type snapYaml struct {
	Name             string                 `yaml:"name"`
	Version          string                 `yaml:"version"`
	Type             Type                   `yaml:"type"`
	Architectures    []string               `yaml:"architectures,omitempty"`
	Assumes          []string               `yaml:"assumes"`
	Description      string                 `yaml:"description"`
	Summary          string                 `yaml:"summary"`
	LicenseAgreement string                 `yaml:"license-agreement,omitempty"`
	LicenseVersion   string                 `yaml:"license-version,omitempty"`
	Plugs            map[string]interface{} `yaml:"plugs,omitempty"`
	Slots            map[string]interface{} `yaml:"slots,omitempty"`
	Apps             map[string]appYaml     `yaml:"apps,omitempty"`
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
	Command string `yaml:"command"`

	Daemon string `yaml:"daemon"`

	StopCommand     string          `yaml:"stop-command,omitempty"`
	PostStopCommand string          `yaml:"post-stop-command,omitempty"`
	StopTimeout     timeout.Timeout `yaml:"stop-timeout,omitempty"`

	RestartCond systemd.RestartCondition `yaml:"restart-condition,omitempty"`
	SlotNames   []string                 `yaml:"slots,omitempty"`
	PlugNames   []string                 `yaml:"plugs,omitempty"`

	BusName string `yaml:"bus-name,omitempty"`

	Socket       bool   `yaml:"socket,omitempty"`
	ListenStream string `yaml:"listen-stream,omitempty"`
	SocketMode   string `yaml:"socket-mode,omitempty"`
}

// InfoFromSnapYaml creates a new info based on the given snap.yaml data
func InfoFromSnapYaml(yamlData []byte) (*Info, error) {
	var y snapYaml
	err := yaml.Unmarshal(yamlData, &y)
	if err != nil {
		return nil, fmt.Errorf("info failed to parse: %s", err)
	}
	// Defaults
	architectures := []string{"all"}
	if len(y.Architectures) != 0 {
		architectures = y.Architectures
	}
	typ := TypeApp
	if y.Type != "" {
		typ = y.Type
	}
	// Construct snap skeleton, without apps, plugs and slots
	snap := &Info{
		SuggestedName:       y.Name,
		Version:             y.Version,
		Type:                typ,
		Architectures:       architectures,
		Assumes:             y.Assumes,
		OriginalDescription: y.Description,
		OriginalSummary:     y.Summary,
		LicenseAgreement:    y.LicenseAgreement,
		LicenseVersion:      y.LicenseVersion,
		Apps:                make(map[string]*AppInfo),
		Plugs:               make(map[string]*PlugInfo),
		Slots:               make(map[string]*SlotInfo),
	}
	sort.Strings(snap.Assumes)
	// Collect top-level definitions of plugs
	for name, data := range y.Plugs {
		iface, label, attrs, err := convertToSlotOrPlugData("plug", name, data)
		if err != nil {
			return nil, err
		}
		snap.Plugs[name] = &PlugInfo{
			Snap:      snap,
			Name:      name,
			Interface: iface,
			Attrs:     attrs,
			Label:     label,
		}
		if len(y.Apps) > 0 {
			snap.Plugs[name].Apps = make(map[string]*AppInfo)
		}
	}
	// Collect top-level definitions of slots
	for name, data := range y.Slots {
		iface, label, attrs, err := convertToSlotOrPlugData("slot", name, data)
		if err != nil {
			return nil, err
		}
		snap.Slots[name] = &SlotInfo{
			Snap:      snap,
			Name:      name,
			Interface: iface,
			Attrs:     attrs,
			Label:     label,
		}
		if len(y.Apps) > 0 {
			snap.Slots[name].Apps = make(map[string]*AppInfo)
		}
	}
	for appName, yApp := range y.Apps {
		// Collect all apps
		app := &AppInfo{
			Snap:            snap,
			Name:            appName,
			Command:         yApp.Command,
			Daemon:          yApp.Daemon,
			StopTimeout:     yApp.StopTimeout,
			StopCommand:     yApp.StopCommand,
			PostStopCommand: yApp.PostStopCommand,
			RestartCond:     yApp.RestartCond,
			Socket:          yApp.Socket,
			SocketMode:      yApp.SocketMode,
			ListenStream:    yApp.ListenStream,
			BusName:         yApp.BusName,
		}
		if len(y.Plugs) > 0 || len(yApp.PlugNames) > 0 {
			app.Plugs = make(map[string]*PlugInfo)
		}
		if len(y.Slots) > 0 || len(yApp.SlotNames) > 0 {
			app.Slots = make(map[string]*SlotInfo)
		}
		snap.Apps[appName] = app
		// Bind all plugs/slots listed in this app
		for _, plugName := range yApp.PlugNames {
			plug, ok := snap.Plugs[plugName]
			if !ok {
				// Create implicit plug definitions if required
				plug = &PlugInfo{
					Snap:      snap,
					Name:      plugName,
					Interface: plugName,
					Apps:      make(map[string]*AppInfo),
				}
				snap.Plugs[plugName] = plug
			}
			app.Plugs[plugName] = plug
			plug.Apps[appName] = app
		}
		for _, slotName := range yApp.SlotNames {
			slot, ok := snap.Slots[slotName]
			if !ok {
				slot = &SlotInfo{
					Snap:      snap,
					Name:      slotName,
					Interface: slotName,
					Apps:      make(map[string]*AppInfo),
				}
				snap.Slots[slotName] = slot
			}
			app.Slots[slotName] = slot
			slot.Apps[appName] = app
		}
	}
	// Bind plugs/slots that are not app-bound to all apps
	for plugName, plug := range snap.Plugs {
		if len(plug.Apps) == 0 {
			for appName, app := range snap.Apps {
				app.Plugs[plugName] = plug
				plug.Apps[appName] = app
			}
		}
	}
	for slotName, slot := range snap.Slots {
		if len(slot.Apps) == 0 {
			for appName, app := range snap.Apps {
				app.Slots[slotName] = slot
				slot.Apps[appName] = app
			}
		}
	}
	// FIXME: validation of the fields
	return snap, nil
}

func convertToSlotOrPlugData(plugOrSlot, name string, data interface{}) (iface, label string, attrs map[string]interface{}, err error) {
	iface = name
	switch data.(type) {
	case string:
		return data.(string), "", nil, nil
	case nil:
		return name, "", nil, nil
	case map[interface{}]interface{}:
		for keyData, valueData := range data.(map[interface{}]interface{}) {
			key, ok := keyData.(string)
			if !ok {
				err := fmt.Errorf("%s %q has attribute that is not a string (found %T)",
					plugOrSlot, name, keyData)
				return "", "", nil, err
			}
			if strings.HasPrefix(key, "$") {
				err := fmt.Errorf("%s %q uses reserved attribute %q", plugOrSlot, name, key)
				return "", "", nil, err
			}
			switch key {
			case "interface":
				value, ok := valueData.(string)
				if !ok {
					err := fmt.Errorf("interface name on %s %q is not a string (found %T)",
						plugOrSlot, name, valueData)
					return "", "", nil, err
				}
				iface = value
			case "label":
				value, ok := valueData.(string)
				if !ok {
					err := fmt.Errorf("label of %s %q is not a string (found %T)",
						plugOrSlot, name, valueData)
					return "", "", nil, err
				}
				label = value
			default:
				if attrs == nil {
					attrs = make(map[string]interface{})
				}
				attrs[key] = valueData
			}
		}
		return iface, label, attrs, nil
	default:
		err := fmt.Errorf("%s %q has malformed definition (found %T)", plugOrSlot, name, data)
		return "", "", nil, err
	}
}
