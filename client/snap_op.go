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
	"fmt"
	"strings"
)

// AddSnap adds the snap with the given name, returning the UUID of the
// background operation upon success.
func (client *Client) AddSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"install"}`)

	return client.doAsync("POST", path, nil, body)
}

// RemoveSnap removes the snap with the given name, returning the UUID of the
// background operation upon success.
func (client *Client) RemoveSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"remove"}`)

	return client.doAsync("POST", path, nil, body)
}

// RefreshSnap refreshes the snap with the given name, returning the UUID of the
// background operation upon success.
func (client *Client) RefreshSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"update"}`)

	return client.doAsync("POST", path, nil, body)
}

// PurgeSnap purges the snap with the given name, returning the UUID of the
// background operation upon success.
//
// TODO: nuke purge, when we have snapshots/backups done
func (client *Client) PurgeSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"purge"}`)

	return client.doAsync("POST", path, nil, body)
}

// RollbackSnap rolls back the snap with the given name, returning the UUID of
// the background operation upon success.
func (client *Client) RollbackSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"rollback"}`)

	return client.doAsync("POST", path, nil, body)
}

// ActivateSnap activates the snap with the given name, returning the UUID of
// the background operation upon success.
func (client *Client) ActivateSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"activate"}`)

	return client.doAsync("POST", path, nil, body)
}

// DeactivateSnap deactivates the snap with the given name, returning the UUID
// of the background operation upon success.
func (client *Client) DeactivateSnap(name string) (string, error) {
	path := fmt.Sprintf("/2.0/snaps/%s", name)
	body := strings.NewReader(`{"action":"deactivate"}`)

	return client.doAsync("POST", path, nil, body)
}
