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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
)

// StoreAccount returns the full store account info for the specified accountID
func (client *Client) StoreAccount(accountID string) (*snap.StoreAccount, error) {
	assertions, err := client.Known("account", map[string]string{"account-id": accountID})
	if err != nil {
		return nil, err
	}
	switch len(assertions) {
	case 1:
		// happy case, break out of the switch
	case 0:
		return nil, fmt.Errorf("no assertion found for account-id %s", accountID)
	default:
		// unknown how this could happen...
		return nil, fmt.Errorf("multiple assertions for account-id %s", accountID)
	}

	acct, ok := assertions[0].(*asserts.Account)
	if !ok {
		return nil, fmt.Errorf("incorrect type of account assertion returned")
	}
	return &snap.StoreAccount{
		ID:          acct.AccountID(),
		Username:    acct.Username(),
		DisplayName: acct.DisplayName(),
		Validation:  acct.Validation(),
	}, nil
}
