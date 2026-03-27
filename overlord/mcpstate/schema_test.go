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

package mcpstate_test

import (
	"testing"

	"github.com/snapcore/snapd/overlord/mcpstate"
)

func TestValidateResourceReadInput(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"valid resource uri", "snap://info/core", false},
		{"valid encoded resource uri", "snap://info/core%2022", false},
		{"empty uri", "", true},
		{"unknown resource endpoint", "snap://connections/core", false},
		{"unsupported scheme", "file://info/core", true},
		{"missing snap name", "snap://info/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mcpstate.ValidateResourceReadInput(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResourceReadInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
