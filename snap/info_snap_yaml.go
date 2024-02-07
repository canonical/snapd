// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timeout"
)

type snapYaml struct {
	Name            string                   `yaml:"name"`
	Version         string                   `yaml:"version"`
	Type            Type                     `yaml:"type"`
	Architectures   []string                 `yaml:"architectures,omitempty"`
	Assumes         []string                 `yaml:"assumes"`
	Title           string                   `yaml:"title"`
	Description     string                   `yaml:"description"`
	Summary         string                   `yaml:"summary"`
	Provenance      string                   `yaml:"provenance"`
	License         string                   `yaml:"license,omitempty"`
	Epoch           Epoch                    `yaml:"epoch,omitempty"`
	Base            string                   `yaml:"base,omitempty"`
	Confinement     ConfinementType          `yaml:"confinement,omitempty"`
	Environment     strutil.OrderedMap       `yaml:"environment,omitempty"`
	Plugs           map[string]interface{}   `yaml:"plugs,omitempty"`
	Slots           map[string]interface{}   `yaml:"slots,omitempty"`
	Apps            map[string]appYaml       `yaml:"apps,omitempty"`
	Hooks           map[string]hookYaml      `yaml:"hooks,omitempty"`
	Layout          map[string]layoutYaml    `yaml:"layout,omitempty"`
	SystemUsernames map[string]interface{}   `yaml:"system-usernames,omitempty"`
	Links           map[string][]string      `yaml:"links,omitempty"`
	Components      map[string]componentYaml `yaml:"components,omitempty"`

	// TypoLayouts is used to detect the use of the incorrect plural form of "layout"
	TypoLayouts typoDetector `yaml:"layouts,omitempty"`
}

type typoDetector struct {
	Hint string
}

func (td *typoDetector) UnmarshalYAML(func(interface{}) error) error {
	return fmt.Errorf("typo detected: %s", td.Hint)
}

type appYaml struct {
	Aliases []string `yaml:"aliases,omitempty"`

	Command      string   `yaml:"command"`
	CommandChain []string `yaml:"command-chain,omitempty"`

	Daemon      string      `yaml:"daemon"`
	DaemonScope DaemonScope `yaml:"daemon-scope"`

	StopCommand     string          `yaml:"stop-command,omitempty"`
	ReloadCommand   string          `yaml:"reload-command,omitempty"`
	PostStopCommand string          `yaml:"post-stop-command,omitempty"`
	StopTimeout     timeout.Timeout `yaml:"stop-timeout,omitempty"`
	StartTimeout    timeout.Timeout `yaml:"start-timeout,omitempty"`
	WatchdogTimeout timeout.Timeout `yaml:"watchdog-timeout,omitempty"`
	Completer       string          `yaml:"completer,omitempty"`
	RefreshMode     string          `yaml:"refresh-mode,omitempty"`
	StopMode        StopModeType    `yaml:"stop-mode,omitempty"`
	InstallMode     string          `yaml:"install-mode,omitempty"`

	RestartCond  RestartCondition `yaml:"restart-condition,omitempty"`
	RestartDelay timeout.Timeout  `yaml:"restart-delay,omitempty"`
	SlotNames    []string         `yaml:"slots,omitempty"`
	PlugNames    []string         `yaml:"plugs,omitempty"`

	BusName     string   `yaml:"bus-name,omitempty"`
	ActivatesOn []string `yaml:"activates-on,omitempty"`
	CommonID    string   `yaml:"common-id,omitempty"`

	Environment strutil.OrderedMap `yaml:"environment,omitempty"`

	Sockets map[string]socketsYaml `yaml:"sockets,omitempty"`

	After  []string `yaml:"after,omitempty"`
	Before []string `yaml:"before,omitempty"`

	Timer string `yaml:"timer,omitempty"`

	Autostart string `yaml:"autostart,omitempty"`
}

type hookYaml struct {
	PlugNames    []string           `yaml:"plugs,omitempty"`
	SlotNames    []string           `yaml:"slots,omitempty"`
	Environment  strutil.OrderedMap `yaml:"environment,omitempty"`
	CommandChain []string           `yaml:"command-chain,omitempty"`
}

type componentYaml struct {
	Type        ComponentType       `yaml:"type"`
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description"`
	Hooks       map[string]hookYaml `yaml:"hooks,omitempty"`
}

type layoutYaml struct {
	Bind     string `yaml:"bind,omitempty"`
	BindFile string `yaml:"bind-file,omitempty"`
	Type     string `yaml:"type,omitempty"`
	User     string `yaml:"user,omitempty"`
	Group    string `yaml:"group,omitempty"`
	Mode     string `yaml:"mode,omitempty"`
	Symlink  string `yaml:"symlink,omitempty"`
}

type socketsYaml struct {
	ListenStream string      `yaml:"listen-stream,omitempty"`
	SocketMode   os.FileMode `yaml:"socket-mode,omitempty"`
}

// InfoFromSnapYaml creates a new info based on the given snap.yaml data
func InfoFromSnapYaml(yamlData []byte) (*Info, error) {
	return infoFromSnapYaml(yamlData, new(scopedTracker))
}

// scopedTracker helps keeping track of which slots/plugs are scoped
// to apps and hooks.
type scopedTracker struct {
	plugs map[*PlugInfo]bool
	slots map[*SlotInfo]bool
}

func (strk *scopedTracker) init(sizeGuess int) {
	strk.plugs = make(map[*PlugInfo]bool, sizeGuess)
	strk.slots = make(map[*SlotInfo]bool, sizeGuess)
}

func (strk *scopedTracker) markPlug(plug *PlugInfo) {
	strk.plugs[plug] = true
}

func (strk *scopedTracker) markSlot(slot *SlotInfo) {
	strk.slots[slot] = true
}

func (strk *scopedTracker) plug(plug *PlugInfo) bool {
	return strk.plugs[plug]
}

func (strk *scopedTracker) slot(slot *SlotInfo) bool {
	return strk.slots[slot]
}

func infoFromSnapYaml(yamlData []byte, strk *scopedTracker) (*Info, error) {
	var y snapYaml
	// Customize hints for the typo detector.
	y.TypoLayouts.Hint = `use singular "layout" instead of plural "layouts"`
	err := yaml.Unmarshal(yamlData, &y)
	if err != nil {
		return nil, fmt.Errorf("cannot parse snap.yaml: %s", err)
	}

	snap := infoSkeletonFromSnapYaml(y)

	// Collect top-level definitions of plugs and slots
	if err := setPlugsFromSnapYaml(y, snap); err != nil {
		return nil, err
	}
	if err := setSlotsFromSnapYaml(y, snap); err != nil {
		return nil, err
	}

	strk.init(len(y.Apps) + len(y.Hooks))

	if err := setComponentsFromSnapYaml(y, snap, strk); err != nil {
		return nil, err
	}

	// Collect all apps, their aliases and hooks
	if err := setAppsFromSnapYaml(y, snap, strk); err != nil {
		return nil, err
	}
	setHooksFromSnapYaml(y, snap, strk)

	// Bind plugs and slots that are not scoped to all known apps and hooks.
	bindUnscopedPlugs(snap, strk)
	bindUnscopedSlots(snap, strk)

	// Collect layout elements.
	if y.Layout != nil {
		snap.Layout = make(map[string]*Layout, len(y.Layout))
		for path, l := range y.Layout {
			var mode os.FileMode = 0755
			if l.Mode != "" {
				m, err := strconv.ParseUint(l.Mode, 8, 32)
				if err != nil {
					return nil, err
				}
				mode = os.FileMode(m)
			}
			user := "root"
			if l.User != "" {
				user = l.User
			}
			group := "root"
			if l.Group != "" {
				group = l.Group
			}
			snap.Layout[path] = &Layout{
				Snap: snap, Path: path,
				Bind: l.Bind, Type: l.Type, Symlink: l.Symlink, BindFile: l.BindFile,
				User: user, Group: group, Mode: mode,
			}
		}
	}

	// Rename specific plugs on the core snap.
	snap.renameClashingCorePlugs()

	snap.BadInterfaces = make(map[string]string)
	SanitizePlugsSlots(snap)

	// Collect system usernames
	if err := setSystemUsernamesFromSnapYaml(y, snap); err != nil {
		return nil, err
	}

	if err := setLinksFromSnapYaml(y, snap); err != nil {
		return nil, err
	}

	// FIXME: validation of the fields
	return snap, nil
}

// infoSkeletonFromSnapYaml initializes an Info without apps, hook, plugs, or
// slots
func infoSkeletonFromSnapYaml(y snapYaml) *Info {
	// Prepare defaults
	architectures := []string{"all"}
	if len(y.Architectures) != 0 {
		architectures = y.Architectures
	}

	typ := TypeApp
	if y.Type != "" {
		typ = y.Type
	}
	// TODO: once we have epochs transition to the snapd type for real
	if y.Name == "snapd" {
		typ = TypeSnapd
	}

	if len(y.Epoch.Read) == 0 {
		// normalize
		y.Epoch.Read = []uint32{0}
		y.Epoch.Write = []uint32{0}
	}

	confinement := StrictConfinement
	if y.Confinement != "" {
		confinement = y.Confinement
	}

	// Construct snap skeleton without apps, hooks, plugs, or slots
	snap := &Info{
		SuggestedName:       y.Name,
		Version:             y.Version,
		SnapType:            typ,
		Architectures:       architectures,
		Assumes:             y.Assumes,
		OriginalTitle:       y.Title,
		OriginalDescription: y.Description,
		OriginalSummary:     y.Summary,
		SnapProvenance:      y.Provenance,
		License:             y.License,
		Epoch:               y.Epoch,
		Confinement:         confinement,
		Base:                y.Base,
		Apps:                make(map[string]*AppInfo),
		LegacyAliases:       make(map[string]*AppInfo),
		Hooks:               make(map[string]*HookInfo),
		Plugs:               make(map[string]*PlugInfo),
		Slots:               make(map[string]*SlotInfo),
		Environment:         y.Environment,
		SystemUsernames:     make(map[string]*SystemUsernameInfo),
		OriginalLinks:       make(map[string][]string),
	}

	sort.Strings(snap.Assumes)

	return snap
}

func setComponentsFromSnapYaml(y snapYaml, snap *Info, strk *scopedTracker) error {
	if len(y.Components) > 0 {
		snap.Components = make(map[string]*Component, len(y.Components))
	}

	for name, data := range y.Components {
		component := Component{
			Name:        name,
			Type:        data.Type,
			Summary:     data.Summary,
			Description: data.Description,
		}

		if len(data.Hooks) > 0 {
			component.Hooks = make(map[string]*HookInfo, len(data.Hooks))
		}

		for hookName, hookData := range data.Hooks {
			if !IsComponentHookSupported(hookName) {
				return fmt.Errorf("unsupported component hook: %q", hookName)
			}

			componentHook := &HookInfo{
				Snap:         snap,
				Name:         hookName,
				Environment:  hookData.Environment,
				CommandChain: hookData.CommandChain,
				Component:    &component,
				Explicit:     true,
			}

			// TODO: this might need to change one day
			if len(hookData.SlotNames) > 0 {
				return fmt.Errorf("component hooks cannot have slots")
			}

			if len(hookData.PlugNames) > 0 {
				componentHook.Plugs = make(map[string]*PlugInfo, len(hookData.PlugNames))
			}

			bindPlugsToHook(componentHook, hookData.PlugNames, snap, strk)

			component.Hooks[hookName] = componentHook
		}

		snap.Components[name] = &component
	}

	return nil
}

func setPlugsFromSnapYaml(y snapYaml, snap *Info) error {
	for name, data := range y.Plugs {
		iface, label, attrs, err := convertToSlotOrPlugData("plug", name, data)
		if err != nil {
			return err
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

	return nil
}

func setSlotsFromSnapYaml(y snapYaml, snap *Info) error {
	for name, data := range y.Slots {
		iface, label, attrs, err := convertToSlotOrPlugData("slot", name, data)
		if err != nil {
			return err
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

	return nil
}

func setAppsFromSnapYaml(y snapYaml, snap *Info, strk *scopedTracker) error {
	for appName, yApp := range y.Apps {
		// Collect all apps
		app := &AppInfo{
			Snap:            snap,
			Name:            appName,
			LegacyAliases:   yApp.Aliases,
			Command:         yApp.Command,
			CommandChain:    yApp.CommandChain,
			StartTimeout:    yApp.StartTimeout,
			Daemon:          yApp.Daemon,
			DaemonScope:     yApp.DaemonScope,
			StopTimeout:     yApp.StopTimeout,
			StopCommand:     yApp.StopCommand,
			ReloadCommand:   yApp.ReloadCommand,
			PostStopCommand: yApp.PostStopCommand,
			RestartCond:     yApp.RestartCond,
			RestartDelay:    yApp.RestartDelay,
			BusName:         yApp.BusName,
			CommonID:        yApp.CommonID,
			Environment:     yApp.Environment,
			Completer:       yApp.Completer,
			StopMode:        yApp.StopMode,
			RefreshMode:     yApp.RefreshMode,
			InstallMode:     yApp.InstallMode,
			Before:          yApp.Before,
			After:           yApp.After,
			Autostart:       yApp.Autostart,
			WatchdogTimeout: yApp.WatchdogTimeout,
		}
		if len(y.Plugs) > 0 || len(yApp.PlugNames) > 0 {
			app.Plugs = make(map[string]*PlugInfo)
		}
		if len(y.Slots) > 0 || len(yApp.SlotNames) > 0 {
			app.Slots = make(map[string]*SlotInfo)
		}
		if len(yApp.Sockets) > 0 {
			app.Sockets = make(map[string]*SocketInfo, len(yApp.Sockets))
		}
		if len(yApp.ActivatesOn) > 0 {
			app.ActivatesOn = make([]*SlotInfo, 0, len(yApp.ActivatesOn))
		}
		// Daemons default to being system daemons
		if app.Daemon != "" && app.DaemonScope == "" {
			app.DaemonScope = SystemDaemon
		}

		snap.Apps[appName] = app
		for _, alias := range app.LegacyAliases {
			if snap.LegacyAliases[alias] != nil {
				return fmt.Errorf("cannot set %q as alias for both %q and %q", alias, snap.LegacyAliases[alias].Name, appName)
			}
			snap.LegacyAliases[alias] = app
		}
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
			// Mark the plug as scoped.
			strk.markPlug(plug)
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
			// Mark the slot as scoped.
			strk.markSlot(slot)
			app.Slots[slotName] = slot
			slot.Apps[appName] = app
		}
		for _, slotName := range yApp.ActivatesOn {
			slot, ok := snap.Slots[slotName]
			if !ok {
				return fmt.Errorf("invalid activates-on value %q on app %q: slot not found", slotName, appName)
			}
			app.ActivatesOn = append(app.ActivatesOn, slot)
			// Implicitly add the slot to the app
			strk.markSlot(slot)
			app.Slots[slotName] = slot
			slot.Apps[appName] = app
		}
		for name, data := range yApp.Sockets {
			app.Sockets[name] = &SocketInfo{
				App:          app,
				Name:         name,
				ListenStream: data.ListenStream,
				SocketMode:   data.SocketMode,
			}
		}
		if yApp.Timer != "" {
			app.Timer = &TimerInfo{
				App:   app,
				Timer: yApp.Timer,
			}
		}
		// collect all common IDs
		if app.CommonID != "" {
			snap.CommonIDs = append(snap.CommonIDs, app.CommonID)
		}
	}
	return nil
}

func setHooksFromSnapYaml(y snapYaml, snap *Info, strk *scopedTracker) {
	for hookName, yHook := range y.Hooks {
		if !IsHookSupported(hookName) {
			continue
		}

		// Collect all hooks
		hook := &HookInfo{
			Snap:         snap,
			Name:         hookName,
			Environment:  yHook.Environment,
			CommandChain: yHook.CommandChain,
			Explicit:     true,
		}
		if len(y.Plugs) > 0 || len(yHook.PlugNames) > 0 {
			hook.Plugs = make(map[string]*PlugInfo)
		}
		if len(y.Slots) > 0 || len(yHook.SlotNames) > 0 {
			hook.Slots = make(map[string]*SlotInfo)
		}

		snap.Hooks[hookName] = hook

		// Bind all plugs/slots listed in this hook
		bindPlugsToHook(hook, yHook.PlugNames, snap, strk)
		bindSlotsToHook(hook, yHook.SlotNames, snap, strk)
	}
}

func bindSlotsToHook(hook *HookInfo, slotNames []string, snap *Info, strk *scopedTracker) {
	for _, slotName := range slotNames {
		slot, ok := snap.Slots[slotName]
		if !ok {
			// Create implicit slot definitions if required
			slot = &SlotInfo{
				Snap:      snap,
				Name:      slotName,
				Interface: slotName,
			}
			snap.Slots[slotName] = slot
		}

		// Mark the slot as scoped.
		strk.markSlot(slot)

		hook.Slots[slotName] = slot
	}
}

func bindPlugsToHook(hook *HookInfo, plugNames []string, snap *Info, strk *scopedTracker) {
	for _, plugName := range plugNames {
		plug, ok := snap.Plugs[plugName]
		if !ok {
			// Create implicit plug definitions if required
			plug = &PlugInfo{
				Snap:      snap,
				Name:      plugName,
				Interface: plugName,
			}
			snap.Plugs[plugName] = plug
		}

		// Mark the plug as scoped.
		strk.markPlug(plug)

		hook.Plugs[plug.Name] = plug
	}
}

func setSystemUsernamesFromSnapYaml(y snapYaml, snap *Info) error {
	for user, data := range y.SystemUsernames {
		if user == "" {
			return fmt.Errorf("system username cannot be empty")
		}
		scope, attrs, err := convertToUsernamesData(user, data)
		if err != nil {
			return err
		}
		if scope == "" {
			return fmt.Errorf("system username %q does not specify a scope", user)
		}
		snap.SystemUsernames[user] = &SystemUsernameInfo{
			Name:  user,
			Scope: scope,
			Attrs: attrs,
		}
	}

	return nil
}

func setLinksFromSnapYaml(y snapYaml, snap *Info) error {
	for linksKey, links := range y.Links {
		if linksKey == "" {
			return fmt.Errorf("links key cannot be empty")
		}
		if !isValidLinksKey(linksKey) {
			return fmt.Errorf("links key is invalid: %s", linksKey)
		}
		snap.OriginalLinks[linksKey] = links
	}
	return nil
}

func bindUnscopedPlugs(snap *Info, strk *scopedTracker) {
	for plugName, plug := range snap.Plugs {
		if strk.plug(plug) {
			continue
		}

		plug.Unscoped = true

		for appName, app := range snap.Apps {
			app.Plugs[plugName] = plug
			plug.Apps[appName] = app
		}

		for _, hook := range snap.Hooks {
			hook.Plugs[plugName] = plug
		}

		for _, component := range snap.Components {
			for _, componentHook := range component.Hooks {
				if componentHook.Plugs == nil {
					componentHook.Plugs = make(map[string]*PlugInfo)
				}
				componentHook.Plugs[plugName] = plug
			}
		}
	}
}

func bindUnscopedSlots(snap *Info, strk *scopedTracker) {
	for slotName, slot := range snap.Slots {
		if strk.slot(slot) {
			continue
		}

		slot.Unscoped = true

		for appName, app := range snap.Apps {
			app.Slots[slotName] = slot
			slot.Apps[appName] = app
		}
		for _, hook := range snap.Hooks {
			hook.Slots[slotName] = slot
		}
	}
}

// bindImplicitHooks binds all global plugs and slots to implicit hooks
func bindImplicitHooks(snap *Info, strk *scopedTracker) {
	for _, hook := range snap.Hooks {
		if hook.Explicit {
			continue
		}
		for _, plug := range snap.Plugs {
			if strk.plug(plug) {
				continue
			}
			if hook.Plugs == nil {
				hook.Plugs = make(map[string]*PlugInfo)
			}
			hook.Plugs[plug.Name] = plug
		}
		for _, slot := range snap.Slots {
			if strk.slot(slot) {
				continue
			}
			if hook.Slots == nil {
				hook.Slots = make(map[string]*SlotInfo)
			}
			hook.Slots[slot.Name] = slot
		}
	}
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
				err := fmt.Errorf("%s %q has attribute key that is not a string (found %T)",
					plugOrSlot, name, keyData)
				return "", "", nil, err
			}
			if strings.HasPrefix(key, "$") {
				err := fmt.Errorf("%s %q uses reserved attribute %q", plugOrSlot, name, key)
				return "", "", nil, err
			}
			switch key {
			case "":
				return "", "", nil, fmt.Errorf("%s %q has an empty attribute key", plugOrSlot, name)
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
				value, err := metautil.NormalizeValue(valueData)
				if err != nil {
					return "", "", nil, fmt.Errorf("attribute %q of %s %q: %v", key, plugOrSlot, name, err)
				}
				attrs[key] = value
			}
		}
		return iface, label, attrs, nil
	default:
		err := fmt.Errorf("%s %q has malformed definition (found %T)", plugOrSlot, name, data)
		return "", "", nil, err
	}
}

// Short form:
//
//	system-usernames:
//	  snap_daemon: shared  # 'scope' is 'shared'
//	  lxd: external        # currently unsupported
//	  foo: private         # currently unsupported
//
// Attributes form:
//
//	system-usernames:
//	  snap_daemon:
//	    scope: shared
//	    attrib1: ...
//	    attrib2: ...
func convertToUsernamesData(user string, data interface{}) (scope string, attrs map[string]interface{}, err error) {
	switch data.(type) {
	case string:
		return data.(string), nil, nil
	case nil:
		return "", nil, nil
	case map[interface{}]interface{}:
		for keyData, valueData := range data.(map[interface{}]interface{}) {
			key, ok := keyData.(string)
			if !ok {
				err := fmt.Errorf("system username %q has attribute key that is not a string (found %T)", user, keyData)
				return "", nil, err
			}
			switch key {
			case "scope":
				value, ok := valueData.(string)
				if !ok {
					err := fmt.Errorf("scope on system username %q is not a string (found %T)", user, valueData)
					return "", nil, err
				}
				scope = value
			case "":
				return "", nil, fmt.Errorf("system username %q has an empty attribute key", user)
			default:
				if attrs == nil {
					attrs = make(map[string]interface{})
				}
				value, err := metautil.NormalizeValue(valueData)
				if err != nil {
					return "", nil, fmt.Errorf("attribute %q of system username %q: %v", key, user, err)
				}
				attrs[key] = value
			}
		}
		return scope, attrs, nil
	default:
		err := fmt.Errorf("system username %q has malformed definition (found %T)", user, data)
		return "", nil, err
	}
}
