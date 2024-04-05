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
	var started []string
	for _, service := range inst.Services {
		if err := sysd.Start([]string{service}); err != nil {
			startErrors[service] = err.Error()
			break
		}
		started = append(started, service)
	}
	// If we got any failures, attempt to stop the services we started.
	stopErrors := make(map[string]string)
	if len(startErrors) != 0 {
		for _, service := range started {
			if err := sysd.Stop([]string{service}); err != nil {
				stopErrors[service] = err.Error()
			}
		}
	}
	if len(startErrors) == 0 {
		return SyncResponse(nil)
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
			if err := sysd.ReloadOrRestart([]string{service}); err != nil {
				restartErrors[service] = err.Error()
			}
		} else {
			if err := sysd.Restart([]string{service}); err != nil {
				restartErrors[service] = err.Error()
			}
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
		if err := sysd.Stop([]string{service}); err != nil {
			stopErrors[service] = err.Error()
		}
	}
	if len(stopErrors) == 0 {
		return SyncResponse(nil)
	}
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

func serviceDaemonReload(inst *client.ServiceInstruction, sysd systemd.Systemd) Response {
	if len(inst.Services) != 0 {
		return InternalError("daemon-reload should not be called with any services")
	}
	if err := sysd.DaemonReload(); err != nil {
		return InternalError("cannot reload daemon: %v", err)
	}
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
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false, BadRequest("cannot parse content type: %v", err)
	}

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
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into service instruction: %v", err)
	}
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
		sts, err := sysd.Status([]string{service})
		if err != nil {
			statusErrors[service] = err.Error()
			continue
		}
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
	if err := decoder.Decode(&refreshInfo); err != nil {
		return BadRequest("cannot decode request body into pending snap refresh info: %v", err)
	}

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
		if err := parser.ReadFile(desktopFilePath); err == nil {
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

	// TODO: silently ignore error returned when the notification server does not exist.
	// TODO: track returned notification ID and respond to actions, if supported.
	if err := c.s.notificationMgr.SendNotification(notification.ID(refreshInfo.InstanceName), msg); err != nil {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Status: 500,
			Result: &errorResult{
				Message: fmt.Sprintf("cannot send notification message: %v", err),
			},
		})
	}
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
		if err := parser.ReadFile(mainApp.DesktopFile()); err == nil {
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
		if err := parser.ReadFile(app.DesktopFile()); err == nil {
			if icon, err = parser.Get("Desktop Entry", "Icon"); err == nil && icon != "" {
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
	if err := decoder.Decode(&finishRefresh); err != nil {
		return BadRequest("cannot decode request body into finish refresh notification info: %v", err)
	}

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
	if si, err := snap.ReadCurrentInfo(finishRefresh.InstanceName); err == nil {
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
	if err := c.s.notificationMgr.SendNotification(notification.ID(finishRefresh.InstanceName), msg); err != nil {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Status: 500,
			Result: &errorResult{
				Message: fmt.Sprintf("cannot send notification message: %v", err),
			},
		})
	}
	return SyncResponse(nil)
}
