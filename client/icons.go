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

	"github.com/ddkwork/golibrary/mylog"
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

	response, cancel := mylog.Check3(c.rawWithTimeout(context.Background(), "GET", fmt.Sprintf("/v2/icons/%s/icon", pkgID), nil, nil, nil, nil))

	defer cancel()
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("%s: Not Found", errPrefix)
	}

	matches := contentDispositionMatcher(response.Header.Get("Content-Disposition"))

	if matches == nil || matches[1] == "" {
		return nil, fmt.Errorf("%s: cannot determine filename", errPrefix)
	}

	content := mylog.Check2(io.ReadAll(response.Body))

	icon := &Icon{
		Filename: matches[1],
		Content:  content,
	}

	return icon, nil
}
