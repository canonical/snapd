// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"encoding/json"
	"io"
	"net/url"

	"github.com/ddkwork/golibrary/mylog"
)

// SetDoer sets the client's doer to the given one
func (client *Client) SetDoer(d doer) {
	client.doer = d
}

type DoOptions = doOptions

// Do does do.
func (client *Client) Do(method, path string, query url.Values, body io.Reader, v interface{}, opts *DoOptions) (statusCode int, err error) {
	return client.do(method, path, query, nil, body, v, opts)
}

// expose parseError for testing
var ParseErrorInTest = parseError

// expose read and write auth helpers for testing
var (
	TestWriteAuth         = writeAuthData
	TestReadAuth          = readAuthData
	TestStoreAuthFilename = storeAuthDataFilename
)

var TestAuthFileEnvKey = authFileEnvKey

func UnmarshalSnapshotAction(body io.Reader) (act snapshotAction, err error) {
	mylog.Check(json.NewDecoder(body).Decode(&act))
	return
}

type DownloadAction = downloadAction

func MockStdinReadLimit(new int64) (restore func()) {
	oldStdinReadLimit := stdinReadLimit
	stdinReadLimit = new
	return func() {
		stdinReadLimit = oldStdinReadLimit
	}
}
