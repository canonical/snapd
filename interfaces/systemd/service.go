// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// Service describes a single systemd service file
type Service struct {
	Description     string
	Type            string
	RemainAfterExit bool
	ExecStart       string
	ExecStop        string
	Wants           string
	WantedBy        string
	After           string
	Before          string
}

func (s *Service) unitSectionNeeded() bool {
	return s.Description != "" || s.Wants != "" || s.After != "" || s.Before != ""
}

func (s *Service) String() string {
	var buf bytes.Buffer
	if s.unitSectionNeeded() {
		buf.WriteString("[Unit]\n")
	}
	if s.Description != "" {
		fmt.Fprintf(&buf, "Description=%s\n\n", s.Description)
	}
	if s.Wants != "" {
		fmt.Fprintf(&buf, "Wants=%s\n", s.Wants)
	}
	if s.After != "" {
		fmt.Fprintf(&buf, "After=%s\n", s.After)
	}
	if s.Before != "" {
		fmt.Fprintf(&buf, "Before=%s\n", s.Before)
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
	if s.WantedBy != "" {
		fmt.Fprintf(&buf, "WantedBy=%s\n", s.WantedBy)
	}
	return buf.String()
}
