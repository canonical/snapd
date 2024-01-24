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

package notification

import (
	"context"
	"strings"
	"time"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"github.com/snapcore/snapd/logger"
)

const (
	gtkBusName    = "org.gtk.Notifications"
	gtkObjectPath = "/org/gtk/Notifications"
	gtkInterface  = "org.gtk.Notifications"
)

type gtkBackend struct {
	conn        *dbus.Conn
	manager     dbus.BusObject
	desktopID   string
	firstUse    time.Time
	disableIdle bool
	gapp        gapplicationType
}

func (srv *gtkBackend) IdleIsDisabled() bool {
	return srv.disableIdle
}

type gapplicationType struct {
	channel chan ([]interface{})
}

func (gapp *gapplicationType) Activate(platformData map[string]interface{}) *dbus.Error {
	logger.Noticef("Receive DBus Activate signal")
	return nil
}

func (gapp *gapplicationType) Open(uris []string, platformData map[string]interface{}) *dbus.Error {
	logger.Noticef("Receive DBus Open signal")
	return nil
}

func (gapp *gapplicationType) ActivateAction(action string, parameters []interface{}, platformData map[string]interface{}) *dbus.Error {
	logger.Noticef("Received DBus ActivateAction " + action)
	// All the actions triggered by a notification use the same "action" field, "Notification",
	// to allow to add in the future more actions using the org.freedesktop.Application
	// interface.
	if action == "Notification" {
		// We use a channel to ensure that the observer method runs in the same thread where it
		// is created
		gapp.channel <- parameters
	}
	return nil
}

func createOrgFreedesktopApplicationInterface() []introspect.Interface {
	return []introspect.Interface{
		introspect.IntrospectData,
		{
			Name: "org.freedesktop.Application",
			Methods: []introspect.Method{
				{
					Name: "Activate",
					Args: []introspect.Arg{
						{
							Name:      "platform_data",
							Type:      "a{sv}",
							Direction: "in",
						},
					},
				},
				{
					Name: "ActivateAction",
					Args: []introspect.Arg{
						{
							Name:      "action_name",
							Type:      "s",
							Direction: "in",
						}, {
							Name:      "parameter",
							Type:      "av",
							Direction: "in",
						}, {
							Name:      "platform_data",
							Type:      "a{sv}",
							Direction: "in",
						},
					},
				},
				{
					Name: "Open",
					Args: []introspect.Arg{
						{
							Name:      "uris",
							Type:      "as",
							Direction: "in",
						}, {
							Name:      "platform_data",
							Type:      "a{sv}",
							Direction: "in",
						},
					},
				},
			},
		},
	}
}

// GracefulShutdown has nothing to do in this backend
func (srv *gtkBackend) GracefulShutdown() {}

// TODO: support actions via session agent.
var newGtkBackend = func(conn *dbus.Conn, desktopID string) (NotificationManager, error) {
	// If the D-Bus service is not already running, assume it is
	// not available.
	// Use owner to verify that the return values of the method call have the
	// types we expect, which is generally a good validity check.
	var owner string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, gtkBusName).Store(&owner); err != nil {
		return nil, err
	}

	b := &gtkBackend{
		conn:      conn,
		manager:   conn.Object(gtkBusName, gtkObjectPath),
		desktopID: desktopID,
		firstUse:  time.Now(),
		gapp: gapplicationType{
			channel: make(chan []interface{}, 10),
		},
	}

	// Create the org.freedesktop.Application interface
	// The application object must be like the desktopID, but in object form
	// (thus, io.snapcraft.SessionAgent -> /io/snapcraft/SessionAgent)
	gapplicationObjectPath := "/" + strings.ReplaceAll(desktopID, ".", "/")
	var gapplicationNode = &introspect.Node{
		Name:       gapplicationObjectPath,
		Interfaces: createOrgFreedesktopApplicationInterface(),
	}
	// And export it, along with the Introspectable interface
	conn.Export(&b.gapp, dbus.ObjectPath(gapplicationObjectPath), "org.freedesktop.Application")
	conn.Export(introspect.NewIntrospectable(gapplicationNode), dbus.ObjectPath(gapplicationObjectPath), "org.freedesktop.DBus.Introspectable")
	return b, nil
}

func gtkPriority(priority Priority) string {
	switch priority {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityUrgent:
		return "urgent"
	default:
		return "normal" // default to normal
	}
}

type icon struct {
	Type  string
	Value dbus.Variant
}

func createButtons(id ID, actions []Action, info map[string]dbus.Variant) {
	var result []map[string]dbus.Variant

	for _, action := range actions {
		entry := make(map[string]dbus.Variant)
		// the ActivateAction method in org.freedesktop.Application
		// interface doesn't support passing on the notification ID, so
		// we add it as the first parameter. Also, the second parameter
		// will be the action. The action name to be used will be always
		// "Notification". This allows to share the ApplicationAction
		// handler with other actions
		parameters := []string{
			string(id),
			action.ActionKey,
		}
		if action.Parameters != nil {
			for _, parameter := range action.Parameters {
				parameters = append(parameters, parameter)
			}
		}
		if action.ActionKey == "default" {
			// The "default" action is the one triggered when the
			// user clicks on the notification itself, either when
			// it is being shown at the top of the screen with the
			// buttons, or when it is clicked in the list of "old"
			// notifications.
			info["default-action"] = dbus.MakeVariant("app.Notification")
			info["default-action-target"] = dbus.MakeVariant(parameters)
		} else {
			entry["label"] = dbus.MakeVariant(action.LocalizedText)
			// The specification is not very clear about this, but to the
			// button actions to work, they must be preffixed by "app.". Without
			// it, no action is triggered when the user clicks the button.
			entry["action"] = dbus.MakeVariant("app.Notification")
			entry["target"] = dbus.MakeVariant(parameters)
			result = append(result, entry)
		}
	}
	if len(result) != 0 {
		info["buttons"] = dbus.MakeVariant(result)
	}
}

func (srv *gtkBackend) SendNotification(id ID, msg *Message) error {
	info := make(map[string]dbus.Variant)
	if msg.Title != "" {
		info["title"] = dbus.MakeVariant(msg.Title)
	}
	if msg.Body != "" {
		info["body"] = dbus.MakeVariant(msg.Body)
	}
	if msg.Icon != "" {
		icon := icon{Type: "file", Value: dbus.MakeVariant(msg.Icon)}
		info["icon"] = dbus.MakeVariant(icon)
	}
	if msg.Actions != nil {
		createButtons(id, msg.Actions, info)
	}
	info["priority"] = dbus.MakeVariant(gtkPriority(msg.Priority))
	call := srv.manager.Call(gtkInterface+".AddNotification", 0, srv.desktopID, id, info)
	return call.Store()
}

func (srv *gtkBackend) CloseNotification(id ID) error {
	call := srv.manager.Call(gtkInterface+".RemoveNotification", 0, srv.desktopID, id)
	return call.Store()
}

func (srv *gtkBackend) HandleNotifications(ctx context.Context, observer Observer) error {

	return srv.observeNotifications(ctx, observer)
}

func (srv *gtkBackend) observeNotifications(ctx context.Context, observer Observer) error {
	if observer == nil {
		logger.Noticef("Tried to handle notifications without an observer")
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case data, ok := <-srv.gapp.channel:
			if !ok {
				return nil
			}
			if err := srv.processNotificationSignal(data, observer); err != nil {
				return err
			}
		}
	}
}

func (srv *gtkBackend) processNotificationSignal(data []interface{}, observer Observer) error {
	var params []string
	var notificationId string
	var actionKey string
	// For some reason, we are receiving as parameters an array with a single
	// element, which is an array of strings with all the parameters
	for index, param := range data[0].([]string) {
		switch index {
		case 0:
			notificationId = param
			break
		case 1:
			actionKey = param
			break
		default:
			params = append(params, param)
			break
		}
	}
	if observer != nil {
		observer.ActionInvoked(ID(notificationId), actionKey, params)
	}
	return nil
}

func (srv *gtkBackend) IdleDuration() time.Duration {
	return time.Since(srv.firstUse)
}
