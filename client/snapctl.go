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
	"fmt"
	"io"

	"github.com/ddkwork/golibrary/mylog"
)

// InternalSnapctlCmdNeedsStdin returns true if the given snapctl command
// needs data from stdin
func InternalSnapctlCmdNeedsStdin(name string) bool {
	switch name {
	case "fde-setup-result":
		return true
	default:
		return false
	}
}

// SnapCtlOptions holds the various options with which snapctl is invoked.
type SnapCtlOptions struct {
	// ContextID is a string used to determine the context of this call (e.g.
	// which context and handler should be used, etc.)
	ContextID string `json:"context-id"`

	// Args contains a list of parameters to use for this invocation.
	Args []string `json:"args"`
}

// SnapCtlPostData is the data posted to the daemon /v2/snapctl endpoint
// TODO: this can be removed again once we no longer need to pass stdin data
// but instead use a real stdin stream
type SnapCtlPostData struct {
	SnapCtlOptions

	Stdin []byte `json:"stdin,omitempty"`
}

type snapctlOutput struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// protect against too much data via stdin
var stdinReadLimit = int64(4 * 1000 * 1000)

// RunSnapctl requests a snapctl run for the given options.
func (client *Client) RunSnapctl(options *SnapCtlOptions, stdin io.Reader) (stdout, stderr []byte, err error) {
	// TODO: instead of reading all of stdin here we need to forward it to
	//       the daemon eventually
	var stdinData []byte
	if stdin != nil {
		limitedStdin := &io.LimitedReader{R: stdin, N: stdinReadLimit + 1}
		stdinData = mylog.Check2(io.ReadAll(limitedStdin))

		if limitedStdin.N <= 0 {
			return nil, nil, fmt.Errorf("cannot read more than %v bytes of data from stdin", stdinReadLimit)
		}
	}

	b := mylog.Check2(json.Marshal(SnapCtlPostData{
		SnapCtlOptions: *options,
		Stdin:          stdinData,
	}))

	var output snapctlOutput
	_ = mylog.Check2(client.doSync("POST", "/v2/snapctl", nil, nil, bytes.NewReader(b), &output))

	return []byte(output.Stdout), []byte(output.Stderr), nil
}
