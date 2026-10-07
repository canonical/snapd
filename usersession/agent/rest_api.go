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
	"time"

	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/usersession/client"
)

var restApi = []*Command{
	rootCmd,
	sessionInfoCmd,
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
	m := map[string]any{
		"version": c.s.Version,
	}
	return SyncResponse(m)
}

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

var currentLocale = i18n.CurrentLocale

func getLocalizedAppNameFromDesktopFile(parser *goconfigparser.ConfigParser, defaultName string) string {
	// First try with full locale string (e.g. es_ES)
	locale := fmt.Sprintf("Name[%s]", currentLocale())
	if name, err := parser.Get("Desktop Entry", locale); err == nil && name != "" {
		return name
	}

	// If not found, try with the country part
	locale = fmt.Sprintf("Name[%s]", strings.Split(currentLocale(), "_")[0])
	if name, err := parser.Get("Desktop Entry", locale); err == nil && name != "" {
		return name
	}

	// If neither are found, try with the untranslated name
	if name, err := parser.Get("Desktop Entry", "Name"); err == nil && name != "" {
		return name
	}

	return defaultName
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
	var urgencyLevel notification.Urgency
	var body, icon, name string
	var hints []notification.Hint

	snapname, instanceKey := snap.SplitInstanceName(refreshInfo.InstanceName)
	// If we have a desktop file of the busy application, use that apps's icon and name, if possible
	if refreshInfo.BusyAppDesktopEntry != "" {
		parser := goconfigparser.New()
		desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, refreshInfo.BusyAppDesktopEntry+".desktop")
		if err := parser.ReadFile(desktopFilePath); err == nil {
			icon, _ = parser.Get("Desktop Entry", "Icon")
			name = combineNameAndKey(getLocalizedAppNameFromDesktopFile(parser, snapname), instanceKey)
		}
	}
	if name == "" {
		name = combineNameAndKey(snapname, instanceKey)
	}

	summary := fmt.Sprintf(i18n.G("Update available for %s."), name)

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
		summary = fmt.Sprintf(i18n.G("%s is updating now!"), name)
		urgencyLevel = notification.CriticalUrgency
	}
	hints = append(hints, notification.WithUrgency(urgencyLevel))
	// The notification is provided by snapd session agent.
	hints = append(hints, notification.WithDesktopEntry("io.snapcraft.SessionAgent"))

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

func guessAppData(si *snap.Info, defaultName string, instanceKey string) (icon string, name string) {
	parser := goconfigparser.New()

	// trivial heuristic, if the app is named like a snap then
	// it's considered to be the main user facing app and hopefully carries
	// a nice icon
	mainApp, ok := si.Apps[si.SnapName().String()]
	if ok && !mainApp.IsService() {
		// got the main app, grab its desktop file
		if err := parser.ReadFile(mainApp.DesktopFile()); err == nil {
			name = combineNameAndKey(getLocalizedAppNameFromDesktopFile(parser, defaultName), instanceKey)
			icon, _ = parser.Get("Desktop Entry", "Icon")
		}
	}

	if icon != "" {
		return icon, name
	}

	// If it doesn't exist, take the first app in the snap with a DesktopFile with icon
	for _, app := range si.Apps {
		if app.IsService() || app.Name == si.SnapName().String() {
			continue
		}
		if err := parser.ReadFile(app.DesktopFile()); err == nil {
			name = combineNameAndKey(getLocalizedAppNameFromDesktopFile(parser, defaultName), instanceKey)
			if icon, err = parser.Get("Desktop Entry", "Icon"); err == nil && icon != "" {
				break
			}
		}
	}

	return icon, name
}

func combineNameAndKey(name, key string) string {
	if key != "" {
		return fmt.Sprintf("%s (%s)", name, key)
	} else {
		return name
	}
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

	var icon string
	name, instanceKey := snap.SplitInstanceName(finishRefresh.InstanceName)
	if si, err := snap.ReadCurrentInfo(finishRefresh.InstanceName); err == nil {
		icon, name = guessAppData(si, name, instanceKey)
	} else {
		logger.Noticef("cannot load snap-info for %s: %v", combineNameAndKey(name, instanceKey), err)
	}
	if name == "" {
		name = combineNameAndKey(name, instanceKey)
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

	summary := fmt.Sprintf(i18n.G("%s was updated."), name)
	body := i18n.G("Ready to launch.")
	hints := []notification.Hint{
		notification.WithDesktopEntry("io.snapcraft.SessionAgent"),
		notification.WithUrgency(notification.LowUrgency),
	}

	msg := &notification.Message{
		Title: summary,
		Body:  body,
		Hints: hints,
		Icon:  icon,
	}
	if err := c.s.notificationMgr.SendNotification(notification.ID(name), msg); err != nil {
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
