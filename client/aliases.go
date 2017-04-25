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
	"bytes"
	"encoding/json"
)

// aliasAction represents an action performed on aliases.
// With action "unalias" if Snap and Alias are set to the same value,
// snapd will check if what is referred to is indeed a snap or an alias.
type aliasAction struct {
	Action string `json:"action"`
	Snap   string `json:"snap,omitempty"`
	App    string `json:"app,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// performAliasAction performs a single action on aliases.
func (client *Client) performAliasAction(sa *aliasAction) (changeID string, err error) {
	b, err := json.Marshal(sa)
	if err != nil {
		return "", err
	}
	return client.doAsync("POST", "/v2/aliases", nil, nil, bytes.NewReader(b))
}

// Alias sets up a manual alias from alias to app in snapName.
func (client *Client) Alias(snapName, app, alias string) (changeID string, err error) {
	return client.performAliasAction(&aliasAction{
		Action: "alias",
		Snap:   snapName,
		App:    app,
		Alias:  alias,
	})
}

// // DisableAllAliases disables all aliases of a snap, removing all manual ones.
func (client *Client) DisableAllAliases(snapName string) (changeID string, err error) {
	return client.performAliasAction(&aliasAction{
		Action: "unalias",
		Snap:   snapName,
	})
}

// RemoveManualAlias removes a manual alias.
func (client *Client) RemoveManualAlias(alias string) (changeID string, err error) {
	return client.performAliasAction(&aliasAction{
		Action: "unalias",
		Alias:  alias,
	})
}

// Unalias tears down a manual alias or disables all aliases of a snap (removing all manual ones)
func (client *Client) Unalias(aliasOrSnap string) (changeID string, err error) {
	return client.performAliasAction(&aliasAction{
		Action: "unalias",
		Snap:   aliasOrSnap,
		Alias:  aliasOrSnap,
	})
}

// AliasStatus represents the status of an alias.
type AliasStatus struct {
	App    string `json:"app,omitempty"`
	Status string `json:"status,omitempty"`
}

// Aliases returns a map snap -> alias -> AliasStatus for all snaps and aliases in the system.
func (client *Client) Aliases() (allStatuses map[string]map[string]AliasStatus, err error) {
	_, err = client.doSync("GET", "/v2/aliases", nil, nil, nil, &allStatuses)
	return
}
