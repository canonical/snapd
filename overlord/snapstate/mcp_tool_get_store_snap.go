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
	"fmt"
	"strings"
	"time"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
)

const toolGetStoreSnap = "snap_get_store_snap"

type getStoreSnapTool struct{}

type getStoreSnapArgs struct {
	SnapName string `json:"snap_name" mcp:"description=Name of the store snap to query."`
}

type storeChannelEntry struct {
	Channel     string    `json:"channel"`
	Version     string    `json:"version"`
	Revision    int       `json:"revision"`
	Confinement string    `json:"confinement"`
	Size        int       `json:"size"`
	ReleasedAt  time.Time `json:"released_at"`
}

type getStoreSnapResult struct {
	Name          string                       `json:"name"`
	Version       string                       `json:"version"`
	Revision      int                          `json:"revision"`
	Developer     string                       `json:"developer"`
	Title         string                       `json:"title"`
	Summary       string                       `json:"summary"`
	SnapID        string                       `json:"snap_id"`
	StoreURL      string                       `json:"store_url"`
	Description   string                       `json:"description"`
	Type          string                       `json:"type"`
	Confinement   string                       `json:"confinement"`
	License       string                       `json:"license"`
	Base          string                       `json:"base"`
	Architectures []string                     `json:"architectures"`
	Tracks        []string                     `json:"tracks"`
	Publisher     string                       `json:"publisher"`
	Website       string                       `json:"website"`
	Contact       string                       `json:"contact"`
	Channels      map[string]storeChannelEntry `json:"channels"`
}

var getStoreSnapToolDescriptor = mcp.ToolDescriptor{
	Name:         toolGetStoreSnap,
	Title:        "Get store snap details",
	Description:  "Get details about a specific snap from the Snap Store by name (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(getStoreSnapArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(getStoreSnapResult{}),
}

func (getStoreSnapTool) Descriptor() mcp.ToolDescriptor {
	return getStoreSnapToolDescriptor
}

func (getStoreSnapTool) ArgsType() any {
	return &getStoreSnapArgs{}
}

func (getStoreSnapTool) ValidateArgs(args any) error {
	v, ok := args.(*getStoreSnapArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for get store snap tool")
	}
	if strings.TrimSpace(v.SnapName) == "" {
		return fmt.Errorf("snap_name must not be empty")
	}
	return nil
}

func (getStoreSnapTool) ResultType() any {
	return &getStoreSnapResult{}
}

func (getStoreSnapTool) CallWithArgs(ctx context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*getStoreSnapArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for get store snap tool")
	}
	if strings.TrimSpace(filterArgs.SnapName) == "" {
		return nil, fmt.Errorf("snap_name must not be empty")
	}
	snapName := strings.TrimSpace(filterArgs.SnapName)

	storeService, err := storeFromState(st)
	if err != nil {
		return nil, err
	}

	storeSnap, err := storeService.SnapInfo(ctx, store.SnapSpec{Name: snapName}, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot get store snap %q: %w", snapName, err)
	}

	result := getStoreSnapResult{
		Name:          storeSnap.SnapName(),
		Version:       storeSnap.Version,
		Revision:      storeSnap.Revision.N,
		Developer:     storeSnap.Publisher.Username,
		Title:         storeSnap.Title(),
		Summary:       storeSnap.Summary(),
		SnapID:        storeSnap.SnapID,
		StoreURL:      storeSnap.StoreURL,
		Description:   storeSnap.Description(),
		Type:          string(storeSnap.SnapType),
		Confinement:   string(storeSnap.Confinement),
		License:       storeSnap.License,
		Base:          storeSnap.Base,
		Architectures: storeSnap.Architectures,
		Tracks:        storeSnap.Tracks,
		Publisher:     storeSnap.Publisher.DisplayName,
		Website:       firstStoreLink(storeSnap.Links(), "website"),
		Contact:       firstStoreLink(storeSnap.Links(), "contact"),
		Channels:      make(map[string]storeChannelEntry, 0),
	}
	for channelName, channelInfo := range storeSnap.Channels {
		result.Channels[channelName] = storeChannelEntry{
			Channel:     channelInfo.Channel,
			Version:     channelInfo.Version,
			Revision:    channelInfo.Revision.N,
			Confinement: string(channelInfo.Confinement),
			Size:        int(channelInfo.Size),
			ReleasedAt:  channelInfo.ReleasedAt,
		}
	}

	return result, nil
}

func (getStoreSnapTool) Validate(args map[string]any) error {
	_, err := mcp.ToolArgsFromMap[getStoreSnapArgs](args)
	return err
}

func (getStoreSnapTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[getStoreSnapArgs](args)
	if err != nil {
		return nil, err
	}
	return getStoreSnapTool{}.CallWithArgs(ctx, st, parsedArgs)
}
