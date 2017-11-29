// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package progresstest

import (
	"github.com/snapcore/snapd/progress"
)

type Meter struct {
	Labels   []string
	Totals   []float64
	Values   []float64
	Written  [][]byte
	Notices  []string
	Finishes int
}

// interface check
var _ progress.Meter = (*Meter)(nil)

func (p *Meter) Start(label string, total float64) {
	p.Spin(label)
	p.SetTotal(total)
}

func (p *Meter) Set(value float64) {
	p.Values = append(p.Values, value)
}

func (p *Meter) SetTotal(total float64) {
	p.Totals = append(p.Totals, total)
}

func (p *Meter) Finished() {
	p.Finishes++
}

func (p *Meter) Spin(label string) {
	p.Labels = append(p.Labels, label)
}

func (p *Meter) Write(bs []byte) (n int, err error) {
	p.Written = append(p.Written, bs)
	n = len(bs)

	return
}

func (p *Meter) Notify(msg string) {
	p.Notices = append(p.Notices, msg)
}
