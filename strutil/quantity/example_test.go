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

package quantity_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil/quantity"
)

func Test(t *testing.T) { TestingT(t) }

func ExampleFormatAmount_short() {
	fmt.Printf("%q\n", quantity.FormatAmount(12345, -1))
	// Output: "12.3k"
}

func ExampleFormatAmount_long() {
	for _, amount := range []uint64{
		3,
		13, 95,
		103, 995,
		1013, 9995,
		10009, 99995,
	} {
		fmt.Printf("- %5d: 3: %q  5: %q  7: %q\n",
			amount,
			quantity.FormatAmount(amount, 3),
			quantity.FormatAmount(amount, -1),
			quantity.FormatAmount(amount, 7),
		)
	}
	// Output:
	// -     3: 3: "  3"  5: "    3"  7: "     3 "
	// -    13: 3: " 13"  5: "   13"  7: "    13 "
	// -    95: 3: " 95"  5: "   95"  7: "    95 "
	// -   103: 3: "103"  5: "  103"  7: "   103 "
	// -   995: 3: "995"  5: "  995"  7: "   995 "
	// -  1013: 3: " 1k"  5: " 1013"  7: "  1013 "
	// -  9995: 3: "10k"  5: "10.0k"  7: " 9.995k"
	// - 10009: 3: "10k"  5: "10.0k"  7: "10.009k"
	// - 99995: 3: ".1M"  5: " 100k"  7: "100.00k"
}

func ExampleFormatBPS() {
	fmt.Printf("%q\n", quantity.FormatBPS(12345, (10*time.Millisecond).Seconds(), -1))
	// Output: "1.23MB/s"
}

func ExampleFormatDuration() {
	for _, dt := range []time.Duration{
		3 * time.Nanosecond,
		36 * time.Microsecond,
		430 * time.Millisecond,
		5155 * time.Millisecond,
		time.Minute + 2*time.Second,
		124 * time.Minute / 10,
		2*time.Hour + 29*time.Minute,
		10*time.Hour + 9*time.Minute,
		10*time.Hour + 30*time.Minute,
		11*time.Hour + 2*time.Minute,
		30 * time.Hour,
		345 * time.Hour,
		357 * time.Hour,
		4272 * time.Hour,
		51368 * time.Hour,
		math.MaxInt64 / 10,
		math.MaxInt64,
	} {
		fmt.Printf("%q\n", quantity.FormatDuration(dt.Seconds()))
	}
	fmt.Printf("%q\n", quantity.FormatDuration(float64(math.MaxUint64)*365*24*60*60))
	fmt.Printf("%q\n", quantity.FormatDuration(math.MaxFloat64))

	// Output:
	// "3.0ns"
	// " 36µs"
	// "430ms"
	// "5.16s"
	// "1m02s"
	// "12.4m"
	// "2h29m"
	// "10h9m"
	// "10.5h"
	// "11h2m"
	// "1d06h"
	// "14d9h"
	// "14.9d"
	// " 178d"
	// "5.86y"
	// "29.2y"
	// " 292y"
	// " 18Ey"
	// "ages!"
}
