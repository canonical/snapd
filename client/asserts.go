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
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"golang.org/x/xerrors"

	// for parsing
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
)

// Ack tries to add an assertion to the system assertion
// database. To succeed the assertion must be valid, its signature
// verified with a known public key and the assertion consistent with
// and its prerequisite in the database.
func (client *Client) Ack(b []byte) error {
	var rsp interface{}
	mylog.Check2(client.doSync("POST", "/v2/assertions", nil, nil, bytes.NewReader(b), &rsp))

	return nil
}

// AssertionTypes returns a list of assertion type names.
func (client *Client) AssertionTypes() ([]string, error) {
	var types struct {
		Types []string `json:"types"`
	}
	_ := mylog.Check2(client.doSync("GET", "/v2/assertions", nil, nil, nil, &types))

	return types.Types, nil
}

// KnownOptions represent the options of the Known call.
type KnownOptions struct {
	// If Remote is true, the store is queried to find the assertion
	Remote bool
}

// Known queries assertions with type assertTypeName and matching assertion headers.
func (client *Client) Known(assertTypeName string, headers map[string]string, opts *KnownOptions) ([]asserts.Assertion, error) {
	if opts == nil {
		opts = &KnownOptions{}
	}

	path := fmt.Sprintf("/v2/assertions/%s", assertTypeName)
	q := url.Values{}

	if len(headers) > 0 {
		for k, v := range headers {
			q.Set(k, v)
		}
	}
	if opts.Remote {
		q.Set("remote", "true")
	}

	response, cancel := mylog.Check3(client.rawWithTimeout(context.Background(), "GET", path, q, nil, nil, nil))

	defer cancel()
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, parseError(response)
	}

	assertionsCount := mylog.Check2(strconv.Atoi(response.Header.Get("X-Ubuntu-Assertions-Count")))

	dec := asserts.NewDecoder(response.Body)

	asserts := []asserts.Assertion{}

	// TODO: make sure asserts can decode and deal with unknown types
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}

		asserts = append(asserts, a)
	}

	if len(asserts) != assertionsCount {
		return nil, fmt.Errorf("response did not have the expected number of assertions")
	}

	return asserts, nil
}

// StoreAccount returns the full store account info for the specified accountID
func (client *Client) StoreAccount(accountID string) (*snap.StoreAccount, error) {
	assertions := mylog.Check2(client.Known("account", map[string]string{"account-id": accountID}, nil))

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
