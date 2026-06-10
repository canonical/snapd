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
	"strings"
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
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ChangeID string `json:"change-id"`
}

var supportedFeatures = []string{
	"async",
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
		stdinData, err = io.ReadAll(limitedStdin)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read stdin: %v", err)
		}
		if limitedStdin.N <= 0 {
			return nil, nil, fmt.Errorf("cannot read more than %v bytes of data from stdin", stdinReadLimit)
		}
	}

	b, err := json.Marshal(SnapCtlPostData{
		SnapCtlOptions: *options,
		Stdin:          stdinData,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal options: %s", err)
	}

	var header map[string]string

	if len(supportedFeatures) > 0 {
		header = make(map[string]string)
		header["X-Snapctl-Features"] = strings.Join(supportedFeatures, ",")
	}

	var output snapctlOutput
	_, err = client.doSync("POST", "/v2/snapctl", nil, header, bytes.NewReader(b), &output)
	if err != nil {
		return nil, nil, err
	}

	//If a change ID is returned, poll until the change is ready.
	if output.ChangeID != "" {
		pollBody, err := json.Marshal(SnapCtlPostData{
			SnapCtlOptions: SnapCtlOptions{
				ContextID: options.ContextID,
				Args:      []string{"is-ready", output.ChangeID},
			},
			Stdin: stdinData,
		})

		if err != nil {
			return nil, nil, err
		}

		output, err = client.SnapctlPollLoop(pollBody, header)
		if err != nil {
			return []byte(output.Stdout), []byte(output.Stderr), err
		}
	}

	return []byte(output.Stdout), []byte(output.Stderr), nil
}

func (client *Client) SnapctlPollLoop(pollBody []byte, header map[string]string) (snapctlOutput, error) {
	var pollOutput snapctlOutput
	for {
		// Clear pollOutput before each run to avoid inheriting previous stdout/stderr.
		pollOutput = snapctlOutput{}
		_, err := client.doSync("POST", "/v2/snapctl", nil, header, bytes.NewReader(pollBody), &pollOutput)

		if err != nil {
			// If the error is of type unsuccessful with exit code 3,
			// the change is still in progress, continue polling.
			if e, ok := err.(*Error); ok && e.Kind == ErrorKindUnsuccessful {
				if val, ok := e.Value.(map[string]any); ok {
					if num, ok := val["exit-code"].(float64); ok {
						if int64(num) == 3 {
							continue
						}
					}
				}
			}
			// Any other error means something actually failed.
			return pollOutput, err
		}

		if pollOutput.Stderr != "" {
			return pollOutput, fmt.Errorf("snapctl is-ready finished with error: %s", pollOutput.Stderr)
		}

		// If it succeeds and has no error, the change is ready, update output and break out.
		return pollOutput, nil
	}
}
