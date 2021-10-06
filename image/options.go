// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package image

type Options struct {
	ModelFile string
	Classic   bool

	Channel string

	// TODO: use OptionsSnap directly here?
	Snaps        []string
	SnapChannels map[string]string

	// WideCohortKey can be used to supply a cohort covering all
	// the snaps in the image, there is no generally suppported API
	// to create such a cohort key.
	WideCohortKey string

	PrepareDir string

	// Architecture to use if none is specified by the model,
	// useful only for classic mode. If set must match the model otherwise.
	Architecture string

	Customizations Customizations
}

// Customizatons defines possible image customizations. Not all of
// them applies to all kind of systems.
type Customizations struct {
	// ConsoleConf can be set to "disabled" to disable console-conf
	// forcefully (UC16/18 only ATM).
	ConsoleConf string `json:"console-conf"`
	// CloudInitUserData can optionally point to cloud init user-data
	// (UC16/18 only)
	CloudInitUserData string `json:"cloud-init-user-data"`
	// BootFlags can be set to a list of boot flags
	// to set in the recovery bootloader (UC20 only).
	// Currently only the "factory" hint flag is supported.
	BootFlags []string `json:"boot-flags"`
	// Validation controls whether validations should be taken
	// into account by the store to select snap revisions.
	// It can be set to "enforce" or "ignore".
	Validation string `json:"validation"`
}
