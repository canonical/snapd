// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/snap"
)

// Component describes a component for API purposes
// TODO we will eventually add a "status" field when it becomes clear which
// states can a component have.
type Component struct {
	Name          string             `json:"name"`
	Type          snap.ComponentType `json:"type"`
	Version       string             `json:"version"`
	Summary       string             `json:"summary"`
	Description   string             `json:"description"`
	Revision      snap.Revision      `json:"revision"`
	InstalledSize int64              `json:"installed-size,omitempty"`
	InstallDate   time.Time          `json:"install-date,omitempty"`
}
