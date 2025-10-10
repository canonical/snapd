// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package lists

// TODO:GOVERSION: replace with iter.Seq after go 1.23 update.

// Seq is an iterator over sequences of individual values. When called as
// seq(yield), seq calls yield(v) for each value v in the sequence, stopping
// early if yield returns false. See the iter package documentation for more
// details.
type Seq[V any] func(yield func(V) bool)
