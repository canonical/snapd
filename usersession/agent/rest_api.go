// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/usersession/client"
)

var restApi = []*Command{
	rootCmd,
	sessionInfoCmd,
	serviceControlCmd,
	serviceStatusCmd,
	pendingRefreshNotificationCmd,
	finishRefreshNotificationCmd,
}

var (
	rootCmd = &Command{
		Path: "/",
		GET:  nil,
	}

	sessionInfoCmd = &Command{
		Path: "/v1/session-info",
		GET:  sessionInfo,
	}

	serviceControlCmd = &Command{
		Path: "/v1/service-control",
		POST: postServiceControl,
	}

	serviceStatusCmd = &Command{
		Path: "/v1/service-status",
		GET:  serviceStatus,
	}

	pendingRefreshNotificationCmd = &Command{
		Path: "/v1/notifications/pending-refresh",
		POST: postPendingRefreshNotification,
	}

	finishRefreshNotificationCmd = &Command{
		Path: "/v1/notifications/finish-refresh",
		POST: postRefreshFinishedNotification,
	}
)

func sessionInfo(c *Command, r *http.Request) Response {
	m := map[string]interface{}{
		"version": c.s.Version,
	}
	return SyncResponse(m)
}

func serviceStart(inst *client.ServiceInstruction, sysd systemd.Systemd) Response {
	// Refuse to start non-snap services
	for _, service := range inst.Services {
		if !strings.HasPrefix(service, "snap.") {
			return InternalError("cannot start non-snap service %v", service)
		}
	}

	startErrors := make(map[string]string)

	if inst.Enable {
		mylog.Check(sysd.EnableNoReload(inst.Services))

		// Setup undo logic for the enable in case of errors
		defer func() {
			if err == nil && len(startErrors) == 0 {
				return
			}
			mylog.Check(

				// Only log errors in this case to avoid overriding the initial error
				sysd.DisableNoReload(inst.Services))
			mylog.Check(sysd.DaemonReload())
		}()
		mylog.Check(sysd.DaemonReload())

	}

	var started []string
	for _, service := range inst.Services {
		mylog.Check(sysd.Start([]string{service}))

		started = append(started, service)
	}

	if len(startErrors) == 0 {
		return SyncResponse(nil)
	}

	// If we got any failures, attempt to stop the services we started, and
	// then re-disable if enable was requested
	stopErrors := make(map[string]string)
	for _, service := range started {
		mylog.Check(sysd.Stop([]string{service}))
	}

	return SyncResponse(&resp{
		Type:   ResponseTypeError,
		Status: 500,
		Result: &errorResult{
			Message: "some user services failed to start",
			Kind:    errorKindServiceControl,
			Value: map[string]interface{}{
				"start-errors": startErrors,
				"stop-errors":  stopErrors,
			},
		},
	})
}

func serviceRestart(inst *client.ServiceInstruction, sysd systemd.Systemd) Response {
	// Refuse to restart non-snap services
	for _, service := range inst.Services {
		if !strings.HasPrefix(service, "snap.") {
			return InternalError("cannot restart non-snap service %v", service)
		}
	}

	restartErrors := make(map[string]string)
	for _, service := range inst.Services {
		if inst.Reload {
			mylog.Check(sysd.ReloadOrRestart([]string{service}))
		} else {
			mylog.Check(sysd.Restart([]string{service}))
		}
	}
	if len(restartErrors) == 0 {
		return SyncResponse(nil)
	}
	return SyncResponse(&resp{
		Type:   ResponseTypeError,
		Status: 500,
		Result: &errorResult{
			Message: "some user services failed to restart",
			Kind:    errorKindServiceControl,
			Value: map[string]interface{}{
				"restart-errors": restartErrors,
			},
		},
	})
}

func serviceStop(inst *client.ServiceInstruction, sysd systemd.Systemd) Response {
	// Refuse to stop non-snap services
	for _, service := range inst.Services {
		if !strings.HasPrefix(service, "snap.") {
			return InternalError("cannot stop non-snap service %v", service)
		}
	}

	stopErrors := make(map[string]string)
	for _, service := range inst.Services {
		mylog.Check(sysd.Stop([]string{service}))
	}

	if len(stopErrors) != 0 {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Status: 500,
			Result: &errorResult{
				Message: "some user services failed to stop",
				Kind:    errorKindServiceControl,
				Value: map[string]interface{}{
					"stop-errors": stopErrors,
				},
			},
		})
	}

	if inst.Disable {
		mylog.Check(sysd.DisableNoReload(inst.Services))
		mylog.Check(sysd.DaemonReload())

	}
	return SyncResponse(nil)
}

func serviceDaemonReload(inst *client.ServiceInstruction, sysd systemd.Systemd) Response {
	if len(inst.Services) != 0 {
		return InternalError("daemon-reload should not be called with any services")
	}
	mylog.Check(sysd.DaemonReload())

	return SyncResponse(nil)
}

var serviceInstructionDispTable = map[string]func(*client.ServiceInstruction, systemd.Systemd) Response{
	"start":         serviceStart,
	"stop":          serviceStop,
	"restart":       serviceRestart,
	"daemon-reload": serviceDaemonReload,
}

var systemdLock sync.Mutex

type noopReporter struct{}

func (noopReporter) Notify(string) {}

func validateJSONRequest(r *http.Request) (valid bool, errResp Response) {
	contentType := r.Header.Get("Content-Type")
	mediaType, params := mylog.Check3(mime.ParseMediaType(contentType))

	if mediaType != "application/json" {
		return false, BadRequest("unknown content type: %s", contentType)
	}

	charset := strings.ToUpper(params["charset"])
	if charset != "" && charset != "UTF-8" {
		return false, BadRequest("unknown charset in content type: %s", contentType)
	}

	return true, nil
}

func postServiceControl(c *Command, r *http.Request) Response {
	if ok, resp := validateJSONRequest(r); !ok {
		return resp
	}

	decoder := json.NewDecoder(r.Body)
	var inst client.ServiceInstruction
	mylog.Check(decoder.Decode(&inst))

	impl := serviceInstructionDispTable[inst.Action]
	if impl == nil {
		return BadRequest("unknown action %s", inst.Action)
	}
	// Prevent multiple systemd actions from being carried out simultaneously
	systemdLock.Lock()
	defer systemdLock.Unlock()
	sysd := systemd.New(systemd.UserMode, noopReporter{})
	return impl(&inst, sysd)
}

func unitStatusToClientUnitStatus(units []*systemd.UnitStatus) []*client.ServiceUnitStatus {
	var results []*client.ServiceUnitStatus
	for _, u := range units {
		results = append(results, &client.ServiceUnitStatus{
			Daemon:           u.Daemon,
			Id:               u.Id,
			Name:             u.Name,
			Names:            u.Names,
			Enabled:          u.Enabled,
			Active:           u.Active,
			Installed:        u.Installed,
			NeedDaemonReload: u.NeedDaemonReload,
		})
	}
	return results
}

func serviceStatus(c *Command, r *http.Request) Response {
	query := r.URL.Query()
	services := strutil.CommaSeparatedList(query.Get("services"))

	// Refuse to accept any non-snap services
	for _, service := range services {
		if !strings.HasPrefix(service, "snap.") {
			return InternalError("cannot query non-snap service %v", service)
		}
	}

	// Prevent multiple systemd actions from being carried out simultaneously
	systemdLock.Lock()
	defer systemdLock.Unlock()
	sysd := systemd.New(systemd.UserMode, noopReporter{})

	statusErrors := make(map[string]string)
	var stss []*systemd.UnitStatus
	for _, service := range services {
		sts := mylog.Check2(sysd.Status([]string{service}))

		stss = append(stss, sts...)
	}
	if len(statusErrors) > 0 {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Status: 500,
			Result: &errorResult{
				Message: "some user services failed to respond to status query",
				Kind:    errorKindServiceStatus,
				Value: map[string]interface{}{
					"status-errors": statusErrors,
				},
			},
		})
	}
	return SyncResponse(unitStatusToClientUnitStatus(stss))
}

func postPendingRefreshNotification(c *Command, r *http.Request) Response {
	if ok, resp := validateJSONRequest(r); !ok {
		return resp
	}

	decoder := json.NewDecoder(r.Body)

	// pendingSnapRefreshInfo holds information about pending snap refresh provided by snapd.
	var refreshInfo client.PendingSnapRefreshInfo
	mylog.Check(decoder.Decode(&refreshInfo))

	// Note that since the connection is shared, we are not closing it.
	if c.s.bus == nil {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Status: 500,
			Result: &errorResult{
				Message: "cannot connect to the session bus",
			},
		})
	}

	// TODO: this message needs to be crafted better as it's the only thing guaranteed to be delivered.
	summary := fmt.Sprintf(i18n.G("Update available for %s."), refreshInfo.InstanceName)
	var urgencyLevel notification.Urgency
	var body, icon string
	var hints []notification.Hint

	if daysLeft := int(refreshInfo.TimeRemaining.Truncate(time.Hour).Hours() / 24); daysLeft > 0 {
		urgencyLevel = notification.LowUrgency
		body = fmt.Sprintf(
			i18n.NG("Close the application to update now. It will update automatically in %d day.",
				"Close the application to update now. It will update automatically in %d days.", daysLeft), daysLeft)
	} else if hoursLeft := int(refreshInfo.TimeRemaining.Truncate(time.Minute).Minutes() / 60); hoursLeft > 0 {
		urgencyLevel = notification.NormalUrgency
		body = fmt.Sprintf(
			i18n.NG("Close the application to update now. It will update automatically in %d hour.",
				"Close the application to update now. It will update automatically in %d hours.", hoursLeft), hoursLeft)
	} else if minutesLeft := int(refreshInfo.TimeRemaining.Truncate(time.Minute).Minutes()); minutesLeft > 0 {
		urgencyLevel = notification.CriticalUrgency
		body = fmt.Sprintf(
			i18n.NG("Close the application to update now. It will update automatically in %d minute.",
				"Close the application to update now. It will update automatically in %d minutes.", minutesLeft), minutesLeft)
	} else {
		summary = fmt.Sprintf(i18n.G("%s is updating now!"), refreshInfo.InstanceName)
		urgencyLevel = notification.CriticalUrgency
	}
	hints = append(hints, notification.WithUrgency(urgencyLevel))
	// The notification is provided by snapd session agent.
	hints = append(hints, notification.WithDesktopEntry("io.snapcraft.SessionAgent"))
	// But if we have a desktop file of the busy application, use that apps's icon.
	if refreshInfo.BusyAppDesktopEntry != "" {
		parser := goconfigparser.New()
		desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, refreshInfo.BusyAppDesktopEntry+".desktop")
		if mylog.Check(parser.ReadFile(desktopFilePath)); err == nil {
			icon, _ = parser.Get("Desktop Entry", "Icon")
		}
	}

	msg := &notification.Message{
		AppName: refreshInfo.BusyAppName,
		Title:   summary,
		Icon:    icon,
		Body:    body,
		Hints:   hints,
	}
	mylog.Check(

		// TODO: silently ignore error returned when the notification server does not exist.
		// TODO: track returned notification ID and respond to actions, if supported.
		c.s.notificationMgr.SendNotification(notification.ID(refreshInfo.InstanceName), msg))

	return SyncResponse(nil)
}

func guessAppIcon(si *snap.Info) string {
	var icon string
	parser := goconfigparser.New()

	// trivial heuristic, if the app is named like a snap then
	// it's considered to be the main user facing app and hopefully carries
	// a nice icon
	mainApp, ok := si.Apps[si.SnapName()]
	if ok && !mainApp.IsService() {
		// got the main app, grab its desktop file
		if mylog.Check(parser.ReadFile(mainApp.DesktopFile())); err == nil {
			icon, _ = parser.Get("Desktop Entry", "Icon")
		}
	}
	if icon != "" {
		return icon
	}

	// If it doesn't exist, take the first app in the snap with a DesktopFile with icon
	for _, app := range si.Apps {
		if app.IsService() || app.Name == si.SnapName() {
			continue
		}
		if mylog.Check(parser.ReadFile(app.DesktopFile())); err == nil {
			if icon = mylog.Check2(parser.Get("Desktop Entry", "Icon")); err == nil && icon != "" {
				break
			}
		}
	}
	return icon
}

func postRefreshFinishedNotification(c *Command, r *http.Request) Response {
	if ok, resp := validateJSONRequest(r); !ok {
		return resp
	}

	decoder := json.NewDecoder(r.Body)

	var finishRefresh client.FinishedSnapRefreshInfo
	mylog.Check(decoder.Decode(&finishRefresh))

	// Note that since the connection is shared, we are not closing it.
	if c.s.bus == nil {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Status: 500,
			Result: &errorResult{
				Message: "cannot connect to the session bus",
			},
		})
	}

	summary := fmt.Sprintf(i18n.G("%s was updated."), finishRefresh.InstanceName)
	body := i18n.G("Ready to launch.")
	hints := []notification.Hint{
		notification.WithDesktopEntry("io.snapcraft.SessionAgent"),
		notification.WithUrgency(notification.LowUrgency),
	}

	var icon string
	if si := mylog.Check2(snap.ReadCurrentInfo(finishRefresh.InstanceName)); err == nil {
		icon = guessAppIcon(si)
	} else {
		logger.Noticef("cannot load snap-info for %s: %v", finishRefresh.InstanceName, err)
	}

	msg := &notification.Message{
		Title: summary,
		Body:  body,
		Hints: hints,
		Icon:  icon,
	}
	mylog.Check(c.s.notificationMgr.SendNotification(notification.ID(finishRefresh.InstanceName), msg))

	return SyncResponse(nil)
}
