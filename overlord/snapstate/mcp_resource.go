// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

type snapInfoResource struct{}

func (snapInfoResource) Descriptor() mcp.ResourceDescriptor {
	return mcp.ResourceDescriptor{
		URI:         "snap://info/{snap}",
		Name:        "snap info",
		Description: "Read-only JSON details for a specific snap.",
		MimeType:    "application/json",
	}
}

func (snapInfoResource) Pattern() string {
	return "/info/"
}

func (snapInfoResource) Read(_ context.Context, st *state.State, req *http.Request) (any, error) {
	snapName, err := url.PathUnescape(strings.TrimPrefix(req.URL.EscapedPath(), "/info/"))
	if err != nil {
		return nil, fmt.Errorf("invalid snap name in uri")
	}

	snap, err := snapFromState(st, snapName)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, fmt.Errorf("cannot find snap %q", snapName)
	}

	return map[string]any{
		"contents": []map[string]any{{
			"uri":      "snap://" + strings.TrimPrefix(req.URL.EscapedPath(), "/"),
			"mimeType": "application/json",
			"text":     jsonString(snapToMap(snap)),
		}},
	}, nil
}

// jsonString returns a string representation of a value suitable for embedding in JSON.
func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
