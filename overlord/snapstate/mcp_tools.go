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
	"errors"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var readOnlyToolExecution = mcp.ToolExecution{TaskSupport: mcp.ToolTaskSupportForbidden}

func intArg(args map[string]any, key string) (int, error) {
	value, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	floatValue, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	if float64(int(floatValue)) != floatValue {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return int(floatValue), nil
}

func storeSnapSummaryToMap(info *snap.Info) map[string]any {
	return map[string]any{
		"name":      info.SnapName(),
		"version":   info.Version,
		"revision":  info.Revision.N,
		"developer": info.Publisher.Username,
		"title":     info.Title(),
		"summary":   info.Summary(),
		"snap_id":   info.SnapID,
		"store_url": info.StoreURL,
	}
}

func storeSnapDetailsToMap(info *snap.Info) map[string]any {
	result := storeSnapSummaryToMap(info)
	result["description"] = info.Description()
	result["type"] = string(info.SnapType)
	result["confinement"] = string(info.Confinement)
	result["license"] = info.License
	result["base"] = info.Base
	result["architectures"] = info.Architectures
	result["tracks"] = info.Tracks
	if info.Publisher.DisplayName != "" {
		result["publisher"] = info.Publisher.DisplayName
	}
	if website := firstStoreLink(info.Links(), "website"); website != "" {
		result["website"] = website
	}
	if contact := firstStoreLink(info.Links(), "contact"); contact != "" {
		result["contact"] = contact
	}
	if len(info.Channels) > 0 {
		result["channels"] = storeChannelsToMap(info.Channels)
	}
	return result
}

func storeChannelsToMap(channels map[string]*snap.ChannelSnapInfo) map[string]map[string]any {
	names := make([]string, 0, len(channels))
	for name := range channels {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make(map[string]map[string]any, len(channels))
	for _, channelName := range names {
		channelInfo := channels[channelName]
		result[channelName] = map[string]any{
			"channel":     channelInfo.Channel,
			"version":     channelInfo.Version,
			"revision":    channelInfo.Revision.N,
			"confinement": string(channelInfo.Confinement),
			"size":        channelInfo.Size,
			"released_at": channelInfo.ReleasedAt,
		}
	}
	return result
}

func firstStoreLink(links map[string][]string, key string) string {
	values, ok := links[key]
	if !ok || len(values) == 0 {
		return ""
	}
	return values[0]
}

func storeFromState(st *state.State) (_ StoreService, err error) {
	st.Lock()
	defer st.Unlock()

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("cannot access store service: %v", recovered)
		}
	}()

	storeService := Store(st, nil)
	if storeService == nil {
		return nil, errors.New("cannot access store service")
	}

	return storeService, nil
}
