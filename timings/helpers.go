// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package timings

import (
	"fmt"
)

// Run creates, starts and then stops a nested Span under parent Measurer. The nested
// Span is passed to the measured function and can used to create further spans.
func Run(meas Measurer, label, summary string, f func(nestedTiming Measurer)) {
	nested := meas.StartSpan(label, summary)
	f(nested)
	nested.Stop()
}

// StartupTimestampMsg produce snap startup timings message.
func StartupTimestampMsg(stage string) string {
	now := timeNow()
	return fmt.Sprintf(`{"stage":"%s", "time":"%v.%06d"}`,
		stage, now.Unix(), (now.UnixNano()/1e3)%1000000)
}
