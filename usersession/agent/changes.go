// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package agent

import (
	"errors"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/client"
)

const snapChangesIntrospectionXML = `
<interface name='io.snapcraft.SnapChanges'>
	<method name="GetTasks">
		<arg type="s" name="change_id" direction="in" />
		<arg type="a{sv}" name="extra_data" direction="in" />
		<arg type="a{sa{sv}}" name="tasks_data" direction="out" />
	</method>
	<signal name="Change">
		<arg type="s" name="change_id" />
		<arg type="s" name="change_type" />
		<arg type="s" name="change_kind" />
		<arg type="a{sv}" name="metadata" />
	</signal>

</interface>`

// SnapChanges implements the 'io.snapcraft.SnapChanges' DBus interface.
type SnapChanges struct {
	conn *dbus.Conn
}

// Interface returns the name of the interface this object implements
func (s *SnapChanges) Interface() string {
	return "io.snapcraft.SnapChanges"
}

// ObjectPath returns the path that the object is exported as
func (s *SnapChanges) ObjectPath() dbus.ObjectPath {
	return "/io/snapcraft/SnapChanges"
}

// IntrospectionData gives the XML formatted introspection description
// of the DBus service.
func (s *SnapChanges) IntrospectionData() string {
	return snapChangesIntrospectionXML
}

func (s *SnapChanges) GetTasks(changeId string, extraData map[string]dbus.Variant) (*map[string]map[string]dbus.Variant, *dbus.Error) {
	var cliConfig client.Config

	result := make(map[string]map[string]dbus.Variant)

	cli := client.New(&cliConfig)
	if cli == nil {
		return &result, dbus.NewError("Failed to connect to the daemon", nil)
	}

	changeData, err := cli.Change(changeId)
	if err != nil {
		return &result, dbus.NewError(err.Error(), nil)
	}
	for _, task := range changeData.Tasks {
		val := make(map[string]dbus.Variant)
		val["ID"] = dbus.MakeVariant(task.ID)
		val["Kind"] = dbus.MakeVariant(task.Kind)
		val["Summary"] = dbus.MakeVariant(task.Summary)
		val["SnapName"] = dbus.MakeVariant(task.SnapName)
		val["InstanceName"] = dbus.MakeVariant(task.InstanceName)
		val["Revision"] = dbus.MakeVariant(task.Revision)
		val["Status"] = dbus.MakeVariant(task.Status)
		val["ProgressLabel"] = dbus.MakeVariant(task.Progress.Label)
		val["ProgressTotal"] = dbus.MakeVariant(task.Progress.Total)
		val["ProgressDone"] = dbus.MakeVariant(task.Progress.Done)
		result[task.ID] = val
	}
	return &result, nil
}

func (s *SnapChanges) EmitChange(change_id string, change_type string, change_kind string, extraData map[string]dbus.Variant) error {
	if s.conn != nil {
		s.conn.Emit(s.ObjectPath(), "io.snapcraft.SnapChanges.Change", change_id, change_type, change_kind, extraData)
		return nil
	}
	return errors.New("No session bus connection.")
}
