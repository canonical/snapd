// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"
)

type apiMixin struct {
	Addr string `long:"addr" default:"localhost:11028" description:"Fakestore address"`
}

func (a *apiMixin) apiURL(ep string) string {
	return fmt.Sprintf("http://%s/%s", a.Addr, strings.TrimPrefix(ep, "/"))
}

type cmdDebugAPI struct {
}

var shortDebugAPIHelp = "Interact with the fakestore debug API"

var debugAPICmd *flags.Command

func init() {
	var err error
	debugAPICmd, err = parser.AddCommand("debug-api", shortDebugAPIHelp, "", &cmdDebugAPI{})
	if err != nil {
		panic(err)
	}

	if _, err := debugAPICmd.AddCommand("reset", "Reset all debug state", "", &cmdDebugAPIReset{}); err != nil {
		panic(err)
	}
	if _, err := debugAPICmd.AddCommand("kill-request", "Configure connection kill after N bytes", "", &cmdDebugAPIKillRequest{}); err != nil {
		panic(err)
	}
	if _, err := debugAPICmd.AddCommand("stats", "Print debug stats as JSON", "", &cmdDebugAPIStats{}); err != nil {
		panic(err)
	}
}

// reset

type cmdDebugAPIReset struct {
	apiMixin
}

func (x *cmdDebugAPIReset) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	return doDebugPost(x.apiURL("/debug"), map[string]any{"action": "reset"})
}

// kill-request

type cmdDebugAPIKillRequest struct {
	apiMixin

	Positional struct {
		Path  string `positional-arg-name:"endpoint" description:"URL path to apply kill-after to"`
		Bytes int64  `positional-arg-name:"bytes" description:"Number of bytes to serve before killing the connection"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdDebugAPIKillRequest) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	return doDebugPost(x.apiURL("/debug"), map[string]any{
		"action":     "kill-request",
		"kill-path":  x.Positional.Path,
		"kill-after": x.Positional.Bytes,
	})
}

// stats

type cmdDebugAPIStats struct {
	apiMixin
}

func (x *cmdDebugAPIStats) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	url := x.apiURL("/debug")
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("debug API returned %d: %s", resp.StatusCode, msg)
	}
	var result json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, result, "", "    "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err = buf.WriteTo(os.Stdout)
	return err
}

// helpers

func doDebugPost(url string, body map[string]any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("debug API returned %d: %s", resp.StatusCode, msg)
	}
	return nil
}
