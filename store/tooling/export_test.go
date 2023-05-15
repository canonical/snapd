// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package tooling

import (
	"net/url"

	"github.com/snapcore/snapd/store"
)

var (
	ErrRevisionAndCohort = errRevisionAndCohort
	ErrPathInBase        = errPathInBase
)

func (tsto *ToolingStore) Creds() store.Authorizer {
	return tsto.cfg.Authorizer
}

func (tsto *ToolingStore) StoreURL() *url.URL {
	return tsto.cfg.StoreBaseURL
}

func (opts *DownloadSnapOptions) Validate() error {
	return opts.validate()
}
