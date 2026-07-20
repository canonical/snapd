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
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
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

	// If a change ID is returned, poll until the change is ready.
	if output.ChangeID != "" {
		err = client.snapctlPollLoop(output.ChangeID, options.ContextID, header)
		if err != nil {
			return nil, nil, err
		}
	}

	return []byte(output.Stdout), []byte(output.Stderr), nil
}

func (client *Client) snapctlPollLoop(changeID string, contextID string, header map[string]string) error {
	pollBody, err := json.Marshal(SnapCtlPostData{
		SnapCtlOptions: SnapCtlOptions{
			ContextID: contextID,
			Args:      []string{"is-ready", changeID},
		},
		Stdin: nil,
	})
	if err != nil {
		return errors.New("internal error: cannot marshal poll options")
	}

	for {
		_, err := client.doSync("POST", "/v2/snapctl", nil, header, bytes.NewReader(pollBody), nil)

		// an empty error here implies exit-code==0. in that case, the change is
		// done and successful, proceed.
		if err == nil {
			return nil
		}

		e, ok := err.(*Error)
		if !ok || e.Kind != ErrorKindUnsuccessful {
			return err
		}

		val, ok := e.Value.(map[string]any)
		if !ok {
			return errors.New("internal error: unexpected type")
		}

		num, ok := val["exit-code"].(float64)
		if !ok {
			return errors.New("internal error: unexpected type")
		}

		stderr, _ := val["stderr"].(string)

		switch int64(num) {
		case 1:
			// Failed to get the state of the change. Return the stderr message.
			return errors.New(stderr)
		case 2:
			// Ready, but the change failed. Return the stderr message.
			return errors.New(stderr)
		case 3:
			// Not ready yet, wait and poll again.
			time.Sleep(100 * time.Millisecond)
			continue
		default:
			return fmt.Errorf("internal error: unexpected exit code %d", int64(num))
		}
	}
}
