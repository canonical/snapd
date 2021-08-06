// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package ctlcmd

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

var (
	shortMountHelp = i18n.G("Create a temporary or permanent mount")
	longMountHelp  = i18n.G(`
The mount command mounts the given source onto the given destination path.`)
)

func init() {
	addCommand("mount", shortMountHelp, longMountHelp, func() command { return &mountCommand{} })
}

type mountCommand struct {
	baseCommand
	Positional struct {
		What  string `positional-arg-name:"<what>" required:"yes" description:"path to the resource to be mounted"`
		Where string `positional-arg-name:"<where>" required:"yes" description:"path to the destination mount point"`
	} `positional-args:"yes" required:"yes"`
	Persistent  bool   `long:"persistent" description:"make the mount persist across reboots"`
	Type        string `long:"type" short:"t" description:"partition type"`
	Options     string `long:"options" short:"o" description:"comma-separated list of mount options"`
	snapInfo    *snap.Info
	optionsList []string
}

func matchPathAttribute(path string, attribute interface{}, snapInfo *snap.Info) bool {
	pattern, ok := attribute.(string)
	if !ok {
		return false
	}

	expandedPattern := snapInfo.ExpandSnapVariables(pattern)

	pp, err := utils.NewPathPattern(expandedPattern)
	return err == nil && pp.Matches(path)
}

// matchConnection checks whether the given mount connection attributes give
// the snap permission to execute the mount command
func (m *mountCommand) matchConnection(attributes map[string]interface{}) bool {
	if !matchPathAttribute(m.Positional.What, attributes["what"], m.snapInfo) {
		return false
	}

	if !matchPathAttribute(m.Positional.Where, attributes["where"], m.snapInfo) {
		return false
	}

	if m.Type != "" {
		// TODO: what should we do if the type is not given?
		if types, ok := attributes["type"].([]interface{}); ok {
			found := false
			for _, iface := range types {
				if typeString, ok := iface.(string); ok && typeString == m.Type {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		} else {
			return false
		}
	}

	if optionsIfaces, ok := attributes["options"].([]interface{}); ok {
		var allowedOptions []string
		for _, iface := range optionsIfaces {
			if option, ok := iface.(string); ok {
				allowedOptions = append(allowedOptions, option)
			}
		}
		for _, option := range m.optionsList {
			if !strutil.ListContains(allowedOptions, option) {
				return false
			}
		}
	}

	return true
}

// checkConnections checks whether the established connections give the snap
// permission to execute the mount command
func (m *mountCommand) checkConnections(context *hookstate.Context) error {
	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot get connections: %s", err)
	}

	m.snapInfo, err = snapstate.CurrentInfo(st, snapName)
	if err != nil {
		return fmt.Errorf("internal error: cannot get snap info: %s", err)
	}

	for connId, connState := range conns {
		if connState.Interface != "mount-control" {
			continue
		}

		if connState.Undesired || connState.HotplugGone {
			continue
		}

		connRef, err := interfaces.ParseConnRef(connId)
		if err != nil {
			return err
		}

		if connRef.PlugRef.Snap != snapName {
			continue
		}

		mounts, ok := connState.StaticPlugAttrs["mount"].([]interface{})
		if !ok {
			continue
		}

		for _, mountAttributes := range mounts {
			if m.matchConnection(mountAttributes.(map[string]interface{})) {
				return nil
			}
		}
	}
	return fmt.Errorf("no matching mount-control connection found")
}

func (m *mountCommand) createMountUnit(sysd systemd.Systemd) (string, error) {
	snapName := m.snapInfo.InstanceName()
	revision := m.snapInfo.SnapRevision().String()
	lifetime := systemd.TransientUnit
	if m.Persistent {
		lifetime = systemd.PersistentUnit
	}
	return sysd.AddMountUnitFileWithOptions(&systemd.MountUnitOptions{
		Lifetime: lifetime,
		SnapName: snapName,
		Revision: revision,
		What:     m.Positional.What,
		Where:    m.Positional.Where,
		Fstype:   m.Type,
		Options:  m.optionsList,
	})
}

func (m *mountCommand) Execute([]string) error {
	fmt.Printf("Executing mount command %q -> %q, persistent: %v\n", m.Positional.What, m.Positional.Where, m.Persistent)

	context := m.context()
	if context == nil {
		return fmt.Errorf("cannot mount without a context")
	}

	// Parse the mount options into an array
	for _, option := range strings.Split(m.Options, ",") {
		if option != "" {
			m.optionsList = append(m.optionsList, option)
		}
	}

	if err := m.checkConnections(context); err != nil {
		snapName := context.InstanceName()
		return fmt.Errorf("snap %q lacks permissions to create the requested mount: %v", snapName, err)
	}

	sysd := systemd.New(systemd.SystemMode, nil)
	name, err := m.createMountUnit(sysd)
	if err != nil {
		return fmt.Errorf("Failed to create mount unit: %v", err)
	}

	if err := sysd.Start(name); err != nil {
		// TODO remove mount unit?
		return fmt.Errorf("Failed to start mount unit: %v", err)
	}
	return nil
}
