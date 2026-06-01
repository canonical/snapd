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

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
)

const toolSearchStoreSnaps = "snap_search_store_snaps"

type searchStoreSnapsTool struct{}

type searchStoreSnapsArgs struct {
	Name string `json:"name" mcp:"description=Name or search term to query in the Snap Store."`
}

type storeSnapSummary struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Revision  int    `json:"revision"`
	Developer string `json:"developer"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	SnapID    string `json:"snap_id"`
	StoreURL  string `json:"store_url"`
}

type searchStoreSnapsResult struct {
	StoreSnaps []storeSnapSummary `json:"store_snaps"`
}

var searchStoreSnapsToolDescriptor = mcp.ToolDescriptor{
	Name:         toolSearchStoreSnaps,
	Title:        "Search snaps in the store",
	Description:  "Search for snaps available in the Snap Store by name or search term (read-only).",
	Annotations:  mcp.ToolAnnotations{ReadOnlyHint: true},
	Execution:    readOnlyToolExecution,
	InputSchema:  mcp.InputSchemaFromType(searchStoreSnapsArgs{}),
	OutputSchema: mcp.OutputSchemaFromType(searchStoreSnapsResult{}),
}

func (searchStoreSnapsTool) Descriptor() mcp.ToolDescriptor {
	return searchStoreSnapsToolDescriptor
}

func (searchStoreSnapsTool) ArgsType() any {
	return &searchStoreSnapsArgs{}
}

func (searchStoreSnapsTool) ValidateArgs(args any) error {
	v, ok := args.(*searchStoreSnapsArgs)
	if !ok {
		return fmt.Errorf("invalid typed args for search store snaps tool")
	}
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("name must not be empty")
	}
	return nil
}

func (searchStoreSnapsTool) ResultType() any {
	return &searchStoreSnapsResult{}
}

func (searchStoreSnapsTool) CallWithArgs(ctx context.Context, st *state.State, args any) (any, error) {
	filterArgs, ok := args.(*searchStoreSnapsArgs)
	if !ok {
		return nil, fmt.Errorf("invalid typed args for search store snaps tool")
	}
	query := strings.TrimSpace(filterArgs.Name)
	if query == "" {
		return nil, fmt.Errorf("name must not be empty")
	}

	storeService, err := storeFromState(st)
	if err != nil {
		return nil, err
	}

	storeSnaps, err := storeService.Find(ctx, &store.Search{Query: query}, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot search store snaps: %w", err)
	}

	result := searchStoreSnapsResult{StoreSnaps: make([]storeSnapSummary, 0, len(storeSnaps))}
	for _, info := range storeSnaps {
		result.StoreSnaps = append(result.StoreSnaps, storeSnapSummary{
			Name:      info.SnapName(),
			Version:   info.Version,
			Revision:  info.Revision.N,
			Developer: info.Publisher.Username,
			Title:     info.Title(),
			Summary:   info.Summary(),
			SnapID:    info.SnapID,
			StoreURL:  info.StoreURL,
		})
	}

	return result, nil
}

func (searchStoreSnapsTool) Validate(args map[string]any) error {
	parsedArgs, err := mcp.ToolArgsFromMap[searchStoreSnapsArgs](args)
	if err != nil {
		return err
	}
	return searchStoreSnapsTool{}.ValidateArgs(parsedArgs)
}

func (searchStoreSnapsTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	parsedArgs, err := mcp.ToolArgsFromMap[searchStoreSnapsArgs](args)
	if err != nil {
		return nil, err
	}
	return searchStoreSnapsTool{}.CallWithArgs(ctx, st, parsedArgs)
}
