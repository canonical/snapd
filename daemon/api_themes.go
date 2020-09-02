// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package daemon

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

var (
	themesCmd = &Command{
		Path:   "/v2/themes",
		UserOK: true,
		GET:    checkThemes,
		POST:   tbd,
	}
)

type themeStatus string

const (
	themeInstalled   themeStatus = "installed"
	themeAvailable   themeStatus = "available"
	themeUnavailable themeStatus = "unavailable"
)

type themeStatusResponse struct {
	GtkThemes   map[string]themeStatus `json:"gtk-themes"`
	IconThemes  map[string]themeStatus `json:"icon-themes"`
	SoundThemes map[string]themeStatus `json:"sound-themes"`
}

func getInstalledThemes(d *Daemon) (gtkThemes, iconThemes, soundThemes []string, err error) {
	infos := d.overlord.InterfaceManager().Repository().Info(&interfaces.InfoOptions{
		Names: []string{"content"},
		Slots: true,
	})
	for _, info := range infos {
		for _, slot := range info.Slots {
			var content string
			// The content interface ensures this attribute exists
			if err := slot.Attr("content", &content); err != nil {
				return nil, nil, nil, err
			}
			var themes *[]string
			switch content {
			case "gtk-3-themes":
				themes = &gtkThemes
			case "icon-themes":
				themes = &iconThemes
			case "sound-themes":
				themes = &soundThemes
			default:
				continue
			}
			var sources []interface{}
			if err := slot.Attr("source.read", &sources); err != nil {
				continue
			}
			for _, s := range sources {
				if path, ok := s.(string); ok {
					*themes = append(*themes, filepath.Base(path))
				}
			}
		}
	}
	sort.Strings(gtkThemes)
	sort.Strings(iconThemes)
	sort.Strings(soundThemes)
	return gtkThemes, iconThemes, soundThemes, nil
}

var badPkgCharRegexp = regexp.MustCompile(`[^a-z]+`)

func themePackageCandidates(prefix, themeName string) []string {
	themeName = strings.ToLower(themeName)
	themeName = badPkgCharRegexp.ReplaceAllString(themeName, "-")
	themeName = strings.Trim(themeName, "-")

	var packages []string
	for themeName != "" {
		packages = append(packages, prefix+themeName)
		pos := strings.LastIndexByte(themeName, '-')
		if pos < 0 {
			break
		}
		themeName = themeName[:pos]
	}
	return packages
}

func getThemeStatusForType(ctx context.Context, theStore snapstate.StoreService, user *auth.UserState, prefix string, themes, installed []string, toInstall map[string]*snap.Info) (map[string]themeStatus, error) {
	status := make(map[string]themeStatus, len(themes))

	for _, theme := range themes {
		// Skip duplicates
		if _, ok := status[theme]; ok {
			continue
		}
		if strutil.SortedListContains(installed, theme) {
			status[theme] = themeInstalled
			continue
		}
		status[theme] = themeUnavailable
		for _, name := range themePackageCandidates(prefix, theme) {
			if info, err := theStore.SnapInfo(ctx, store.SnapSpec{Name: name}, user); err == nil {
				// Only mark the theme as available if
				// it has been published to the stable
				// channel.
				if info.Channel == "stable" {
					status[theme] = themeAvailable
					toInstall[name] = info
				}
				break
			} else if err != store.ErrSnapNotFound {
				return nil, err
			}
		}
	}
	return status, nil
}

func getThemeStatus(ctx context.Context, c *Command, user *auth.UserState, gtkThemes, iconThemes, soundThemes []string) (status themeStatusResponse, toInstall map[string]*snap.Info, err error) {
	installedGtk, installedIcon, installedSound, err := getInstalledThemes(c.d)
	if err != nil {
		return themeStatusResponse{}, nil, err
	}

	theStore := getStore(c)
	toInstall = make(map[string]*snap.Info)
	if status.GtkThemes, err = getThemeStatusForType(ctx, theStore, user, "gtk-theme-", gtkThemes, installedGtk, toInstall); err != nil {
		return themeStatusResponse{}, nil, err
	}
	if status.IconThemes, err = getThemeStatusForType(ctx, theStore, user, "icon-theme-", iconThemes, installedIcon, toInstall); err != nil {
		return themeStatusResponse{}, nil, err
	}
	if status.SoundThemes, err = getThemeStatusForType(ctx, theStore, user, "sound-theme-", soundThemes, installedSound, toInstall); err != nil {
		return themeStatusResponse{}, nil, err
	}

	return status, toInstall, nil
}

func checkThemes(c *Command, r *http.Request, user *auth.UserState) Response {
	ctx := store.WithClientUserAgent(r.Context(), r)

	q := r.URL.Query()
	status, _, err := getThemeStatus(ctx, c, user, q["gtk-theme"], q["icon-theme"], q["sound-theme"])
	if err != nil {
		return InternalError("cannot get theme status: %s", err)
	}

	return SyncResponse(status, nil)
}
