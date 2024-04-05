// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package client

import (
	"context"
	"fmt"
	"io"
	"regexp"

	"golang.org/x/xerrors"
)

// Icon represents the icon of an installed snap
type Icon struct {
	Filename string
	Content  []byte
}

var contentDispositionMatcher = regexp.MustCompile(`attachment; filename=(.+)`).FindStringSubmatch

// Icon returns the Icon belonging to an installed snap
func (c *Client) Icon(pkgID string) (*Icon, error) {
	const errPrefix = "cannot retrieve icon"

	response, cancel, err := c.rawWithTimeout(context.Background(), "GET", fmt.Sprintf("/v2/icons/%s/icon", pkgID), nil, nil, nil, nil)
	if err != nil {
		fmt := "%s: failed to communicate with server: %w"
		return nil, xerrors.Errorf(fmt, errPrefix, err)
	}
	defer cancel()
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("%s: Not Found", errPrefix)
	}

	matches := contentDispositionMatcher(response.Header.Get("Content-Disposition"))

	if matches == nil || matches[1] == "" {
		return nil, fmt.Errorf("%s: cannot determine filename", errPrefix)
	}

	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", errPrefix, err)
	}

	icon := &Icon{
		Filename: matches[1],
		Content:  content,
	}

	return icon, nil
}
