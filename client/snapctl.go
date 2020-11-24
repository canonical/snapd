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
	"io/ioutil"
)

// InternalSsnapctlCmdNeedsStdin returns true if the given snapctl command
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

	// stdin dta read for the command (will only be set for some comamnds)
	//
	// TODO: Stdin as bytes should go away, instead the client
	//       should open a connection (or re-using the exisitng
	//       POST) for the real stdin and forward that to the
	//       daemon. There are some complications with that, i.e.
	//       that net.http wants to read the body of a POST and
	//       will hang for stdin. A workaround is to use the transport
	//       directly and to POST. Some PoC code for this:
	//       https://github.com/snapcore/snapd/compare/master...mvo5:snapctl-real-stdin-forwarding-mess?expand=1
	StdinData []byte `json:"stdin-data"`

	// Args contains a list of parameters to use for this invocation.
	Args []string `json:"args"`
}

type snapctlOutput struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// RunSnapctl requests a snapctl run for the given options.
func (client *Client) RunSnapctl(options *SnapCtlOptions, stdin io.Reader) (stdout, stderr []byte, err error) {
	// TODO: instead of reading all of stdin here we need to forward it to
	//       the daemon eventually
	if stdin != nil {
		options.StdinData, err = ioutil.ReadAll(stdin)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read stdin: %v", err)
		}
	}

	b, err := json.Marshal(options)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal options: %s", err)
	}

	var output snapctlOutput
	_, err = client.doSync("POST", "/v2/snapctl", nil, nil, bytes.NewReader(b), &output)
	if err != nil {
		return nil, nil, err
	}

	return []byte(output.Stdout), []byte(output.Stderr), nil
}
