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

package systemd

import (
	"bytes"
	"fmt"
)

// Snippet describes systemd services that interface wishes to create.
// Identical services from all snippets are combined and ignored.
type Snippet struct {
	Services map[string]Service `json:"services,omitempty"`
}

// Service describes a single systemd service file
type Service struct {
	Description     string `json:"description,omitempty"`
	Type            string `json:"type"`
	RemainAfterExit bool   `json:"remain-after-exit,omitempty"`
	ExecStart       string `json:"exec-start,omitempty"`
	ExecStop        string `json:"exec-stop,omitempty"`
}

func (s *Service) String() string {
	var buf bytes.Buffer
	if s.Description != "" {
		buf.WriteString("[Unit]\n")
		fmt.Fprintf(&buf, "Description=%s\n\n", s.Description)
	}
	buf.WriteString("[Service]\n")
	if s.Type != "" {
		fmt.Fprintf(&buf, "Type=%s\n", s.Type)
	}
	// "no" is the default in systemd so we don't neet to  write it
	if s.RemainAfterExit {
		buf.WriteString("RemainAfterExit=yes\n")
	}
	if s.ExecStart != "" {
		fmt.Fprintf(&buf, "ExecStart=%s\n", s.ExecStart)
	}
	if s.ExecStop != "" {
		fmt.Fprintf(&buf, "ExecStop=%s\n", s.ExecStop)
	}
	fmt.Fprintf(&buf, "\n[Install]\nWantedBy=multi-user.target\n")
	return buf.String()
}
