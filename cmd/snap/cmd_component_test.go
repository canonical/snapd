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

package main_test

import (
	"encoding/json"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapOpSuite) TestComponentShowValid(c *check.C) {
	n := 0

	// These embed the real client structs but override fields that need 'snap' types.
	type mockComponent struct {
		client.Component
		Type     string `json:"type"`
		Revision string `json:"revision"`
	}

	type mockSnap struct {
		client.Snap
		Revision   string          `json:"revision"`
		Components []mockComponent `json:"components"`
	}

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(n, check.Equals, 0)
		n++

		comp1 := mockComponent{
			Component: client.Component{
				Name:          "compiler",
				Version:       "1.0",
				Summary:       "The compiler component",
				Description:   "Handles compilation tasks",
				InstalledSize: 200 * 1000 * 1000,
			},
			Type:     "framework",
			Revision: "42",
		}

		comp2 := mockComponent{
			Component: client.Component{
				Name:          "runtime",
				Type:          "app",
				Version:       "1.2",
				Summary:       "The runtime component",
				Description:   "Handles runtime execution",
				InstalledSize: 10 * 1000 * 1000,
			},
			Type:     "app",
			Revision: "10",
		}

		ms := mockSnap{
			Snap: client.Snap{
				Name:          "qwen-vl",
				Version:       "2.0",
				Status:        "active",
				Type:          "app",
				InstalledSize: 10 * 1000 * 1000,
				Description:   "A mock LLM snap",
			},
			Revision:   "100",
			Components: []mockComponent{comp1, comp2},
		}

		resp := map[string]any{
			"type":        "sync",
			"status-code": 200,
			"result":      []mockSnap{ms},
		}
		json.NewEncoder(w).Encode(resp)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"component", "qwen-vl+compiler+runtime"})

	expectedOutput := `component: qwen-vl+compiler
type: framework
summary: The compiler component
description: |
  Handles compilation tasks
installed: 1.0 (42) 200MB
---
component: qwen-vl+runtime
type: app
summary: The runtime component
description: |
  Handles runtime execution
installed: 1.2 (10) 10MB
`

	c.Assert(err, check.IsNil)
	c.Assert(s.Stdout(), check.DeepEquals, expectedOutput)
}

func (s *SnapOpSuite) TestComponentShowSnapNotFound(c *check.C) {
	// Mock a server response returning 0 snaps
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, check.Equals, "/v2/snaps")

		// Return an empty list of snaps
		resp := map[string]any{
			"type":        "sync",
			"status-code": 200,
			"result":      []map[string]any{},
		}
		json.NewEncoder(w).Encode(resp)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"component", "missing-snap+compiler"})

	c.Assert(err, check.ErrorMatches, "no matching snaps installed")
}

func (s *SnapOpSuite) TestComponentShowComponentNotFound(c *check.C) {
	type mockComponent struct {
		client.Component
		Type     string `json:"type"`
		Revision string `json:"revision"`
	}
	type mockSnap struct {
		client.Snap
		Revision   string          `json:"revision"`
		Components []mockComponent `json:"components"`
	}

	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		comp1 := mockComponent{
			Component: client.Component{
				Name:          "compiler",
				Version:       "1.0",
				InstalledSize: 1024,
			},
			Type:     "framework",
			Revision: "42",
		}

		ms := mockSnap{
			Snap: client.Snap{
				Name:    "qwen-vl",
				Version: "2.0",
				Status:  "active",
				Type:    "app",
			},
			Revision:   "100",
			Components: []mockComponent{comp1},
		}

		resp := map[string]any{
			"type":        "sync",
			"status-code": 200,
			"result":      []mockSnap{ms},
		}
		json.NewEncoder(w).Encode(resp)
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"component", "qwen-vl+phantom"})

	c.Assert(err, check.ErrorMatches, `component "phantom" for snap "qwen-vl" is not installed`)
}

func (s *SnapOpSuite) TestComponentInvalidMatrix(c *check.C) {
	invalidMatrix := []struct {
		args        []string
		expectedErr string
	}{
		{
			args:        []string{"qwen-vl+llamacpp", "deepseek-r1+llamacpp-avx512"},
			expectedErr: "exactly one snap and one or more of its components must be specified",
		},
		{
			args:        []string{"+mycomp"},
			expectedErr: "no snap for the component\\(s\\) was specified",
		},
		{
			args:        []string{"mysnap"},
			expectedErr: "no component specified",
		},
		{
			args:        []string{""},
			expectedErr: "argument cannot be empty",
		},
		{
			args:        []string{"test-snap1++arg2"},
			expectedErr: "component name cannot be empty",
		},
		{
			args:        []string{"+"},
			expectedErr: "no snap for the component\\(s\\) was specified",
		},
	}

	for _, tc := range invalidMatrix {
		s.testComponentInvalid(c, tc.args, tc.expectedErr)
	}
}

func (s *SnapOpSuite) testComponentInvalid(c *check.C, cmd []string, expectedErr string) {
	s.RedirectClientToTestServer(nil)

	args := append([]string{"component"}, cmd...)
	_, err := snap.Parser(snap.Client()).ParseArgs(args)

	c.Assert(err, check.ErrorMatches, expectedErr)
}
