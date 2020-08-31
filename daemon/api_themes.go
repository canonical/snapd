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
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/auth"
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

func checkThemes(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	return SyncResponse([]string{"TBD"}, nil)
}
