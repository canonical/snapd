// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package daemon

import (
	"github.com/ubuntu-core/snappy/snappy"
	"github.com/ubuntu-core/snappy/systemd"
)

type tSA struct {
	operr error
	stout []string
	ssout []*snappy.PackageServiceStatus
	lgout []systemd.Log
	llout []string
}

func (t *tSA) Enable() error                                          { return t.operr }
func (t *tSA) Disable() error                                         { return t.operr }
func (t *tSA) Start() error                                           { return t.operr }
func (t *tSA) Stop() error                                            { return t.operr }
func (t *tSA) Restart() error                                         { return t.operr }
func (t *tSA) Status() ([]string, error)                              { return t.stout, t.operr }
func (t *tSA) ServiceStatus() ([]*snappy.PackageServiceStatus, error) { return t.ssout, t.operr }
func (t *tSA) Logs() ([]systemd.Log, error)                           { return t.lgout, t.operr }
func (t *tSA) Loglines() ([]string, error)                            { return t.llout, t.operr }
