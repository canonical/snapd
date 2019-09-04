// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/snap"
)

// StoreAccount returns the full store account info for the specified accountID
func (client *Client) StoreAccount(accountID string) (*snap.StoreAccount, error) {

	asserts, err := client.Known("account", map[string]string{"account-id": accountID})
	if err != nil {
		return nil, err
	}
	switch len(asserts) {
	case 1:
		// happy case, break out of the switch
	case 0:
		return nil, fmt.Errorf("no assertion found for account-id %s", accountID)
	default:
		// unknown how this could happen...
		return nil, fmt.Errorf("multiple assertions for account-id %s", accountID)
	}

	accountAssert := asserts[0]
	return &snap.StoreAccount{
		ID:          accountID,
		Username:    accountAssert.HeaderString("username"),
		DisplayName: accountAssert.HeaderString("display-name"),
		Validation:  accountAssert.HeaderString("validation"),
	}, nil
}
