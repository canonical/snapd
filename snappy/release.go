/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mvo5/goconfigparser"
)

// ReleaseInfo contains a structure with the release information
type ReleaseInfo struct {
	flavor  string
	series  string
	channel string
}

const (
	channelsIni = "/etc/system-image/channels.ini"
)

// SetRelease is used to override the release information determined by the
// the system
func SetRelease(r ReleaseInfo) {
	release = r
}

func releaseInformation(rootDir string) (*ReleaseInfo, error) {
	channelsIniPath := filepath.Join(rootDir, channelsIni)

	cfg := goconfigparser.New()

	if err := cfg.ReadFile(channelsIniPath); err != nil {
		return nil, err
	}

	channel, err := cfg.Get("", "channel")
	if err != nil {
		return nil, err
	}

	channelParts := strings.Split(channel, "/")

	if len(channelParts) != 3 {
		errors.New("using deprecated channels")
	}

	if !strings.HasPrefix(channelParts[0], "ubuntu-") {
		return nil, errors.New("release does not correspond to an ubuntu channel")
	}

	return &ReleaseInfo{
		flavor:  strings.Trim(channelParts[0], "ubuntu-"),
		series:  channelParts[1],
		channel: channelParts[2],
	}, nil
}

// release returns a valid release string to set the store headers
func (r ReleaseInfo) release() string {
	return fmt.Sprintf("%s-%s", r.series, r.flavor)
}
