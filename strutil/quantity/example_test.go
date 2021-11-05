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

var dataset = []float64{
	-math.MaxFloat64,
	float64(math.MinInt64),
	-time.Duration(3 * time.Nanosecond).Seconds(),
	time.Duration(3 * time.Nanosecond).Seconds(),
	time.Duration(36 * time.Microsecond).Seconds(),
	time.Duration(36*time.Microsecond + 3*time.Nanosecond).Seconds(),
	time.Duration(420*time.Millisecond + 36*time.Microsecond + 3*time.Nanosecond).Seconds(),
	time.Duration(430 * time.Millisecond).Seconds(),
	time.Duration(5155 * time.Millisecond).Seconds(),
	time.Duration(time.Minute + 2*time.Second).Seconds(),
	time.Duration(124 * time.Minute / 10).Seconds(),
	time.Duration(2*time.Hour + 29*time.Minute).Seconds(),
	time.Duration(10*time.Hour + 9*time.Minute).Seconds(),
	time.Duration(10*time.Hour + 30*time.Minute).Seconds(),
	time.Duration(10*time.Hour + 30*time.Minute + 2*time.Second).Seconds(),
	time.Duration(11*time.Hour + 2*time.Minute).Seconds(),
	time.Duration(22*time.Hour + 59*time.Minute + 59*time.Second).Seconds(),
	time.Duration(23*time.Hour + 59*time.Minute + 59*time.Second).Seconds(),
	time.Duration(30 * time.Hour).Seconds(),
	time.Duration(345 * time.Hour).Seconds(),
	time.Duration(357 * time.Hour).Seconds(),
	time.Duration(4272 * time.Hour).Seconds(),
	time.Duration(4273*time.Hour + 30*time.Minute + 2*time.Second).Seconds(),
	time.Duration(51368 * time.Hour).Seconds(),
	time.Duration(math.MaxInt64 / 10).Seconds(),
	time.Duration(math.MaxInt64).Seconds(),
	float64(math.MaxUint64) * 365 * 24 * 60 * 60,
	math.MaxFloat64,
}

func Inv(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Year,
		quantity.Sec,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleInv() {

	for _, dt := range dataset {
		Inv(dt)
	}

	// Output:
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
	// "inv!"
}

// Spaces with verbose packing

func SpaceAllTimeLeftRange1(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange1() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange1(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 1h 30m 2s"
	// "5y 314d 2h"
	// "29y 82d 22h 46m 43.685477632s"
	// "292y 98d 23h 47m 16.854775808s"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange2(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Day,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange2() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange2(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 1h 30m 2s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange3(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange3() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange3(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange4(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Min,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange4() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange4(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m 2s"
	// "12m 24s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange5(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Sec,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange5() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange5(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange6(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.NSec,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange6() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange6(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange7(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.USec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange7() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange7(dt)
	}

	// Output:
	// "0µs"
	// "0µs"
	// "0µs"
	// "1µs"
	// "36µs"
	// "37µs"
	// "420.037ms"
	// "430ms"
	// "5.155000s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 1h 30m 2s"
	// "5y 314d 2h"
	// "29y 82d 22h 46m 43.685478s"
	// "292y 98d 23h 47m 16.854776s"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange8(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.MSec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange8() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange8(dt)
	}

	// Output:
	// "0ms"
	// "0ms"
	// "0ms"
	// "1ms"
	// "1ms"
	// "1ms"
	// "421ms"
	// "430ms"
	// "5.155s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 1h 30m 2s"
	// "5y 314d 2h"
	// "29y 82d 22h 46m 43.686s"
	// "292y 98d 23h 47m 16.855s"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange9(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange9() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange9(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 1h 30m 2s"
	// "5y 314d 2h"
	// "29y 82d 22h 46m 44s"
	// "292y 98d 23h 47m 17s"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange10(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Min,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange10() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange10(dt)
	}

	// Output:
	// "0m"
	// "0m"
	// "0m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "2m"
	// "13m"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 1h 31m"
	// "5y 314d 2h"
	// "29y 82d 22h 47m"
	// "292y 98d 23h 48m"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange11(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange11() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange11(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "3h"
	// "11h"
	// "11h"
	// "11h"
	// "12h"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "178d"
	// "178d 2h"
	// "5y 314d 2h"
	// "29y 82d 23h"
	// "292y 99d"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange12(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Day,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange12() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange12(dt)
	}

	// Output:
	// "0d"
	// "0d"
	// "0d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "2d"
	// "15d"
	// "15d"
	// "178d"
	// "179d"
	// "5y 314d"
	// "29y 83d"
	// "292y 99d"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange13(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Year,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange13() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange13(dt)
	}

	// Output:
	// "0y"
	// "0y"
	// "0y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "6y"
	// "30y"
	// "293y"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeLeftRange14(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceAllTimeLeftRange14() {

	for _, dt := range dataset {
		SpaceAllTimeLeftRange14(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 30m 2s"
	// "11h 2m"
	// "22h 59m 59s"
	// "23h 59m 59s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

// Spaces with compact packing

func SpaceCompTimeLeftRange1(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange1() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange1(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange2(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Day,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange2() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange2(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange3(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Hour,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange3() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange3(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange4(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Min,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange4() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange4(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange5(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Sec,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange5() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange5(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange6(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.NSec,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange6() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange6(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange7(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.USec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange7() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange7(dt)
	}

	// Output:
	// "0µs"
	// "0µs"
	// "0µs"
	// "1µs"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange8(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.MSec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange8() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange8(dt)
	}

	// Output:
	// "0ms"
	// "0ms"
	// "0ms"
	// "1ms"
	// "1ms"
	// "1ms"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange9(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange9() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange9(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange10(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Min,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange10() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange10(dt)
	}

	// Output:
	// "0m"
	// "0m"
	// "0m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "2m"
	// "13m"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange11(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange11() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange11(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "3h"
	// "11h"
	// "11h"
	// "11h"
	// "12h"
	// "23h"
	// "1d"
	// "1d 6h"
	// "14d 9h"
	// "14d 21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange12(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Day,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange12() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange12(dt)
	}

	// Output:
	// "0d"
	// "0d"
	// "0d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "2d"
	// "15d"
	// "15d"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange13(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Year,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange13() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange13(dt)
	}

	// Output:
	// "0y"
	// "0y"
	// "0y"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceCompTimeLeftRange14(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Hour,
		quantity.ShowComp,
		quantity.TimeLeft,
		true))
}

func ExampleSpaceCompTimeLeftRange14() {

	for _, dt := range dataset {
		SpaceCompTimeLeftRange14(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m 2s"
	// "12m 24s"
	// "2h 29m"
	// "10h 9m"
	// "10h 30m"
	// "10h 31m"
	// "11h 2m"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

// No spaces with verbose packing

func NoSpaceAllTimeLeftRange1(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange1() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange1(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d1h30m2s"
	// "5y314d2h"
	// "29y82d22h46m43.685477632s"
	// "292y98d23h47m16.854775808s"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange2(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Day,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange2() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange2(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d1h30m2s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange3(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange3() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange3(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange4(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Min,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange4() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange4(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "1m2s"
	// "12m24s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange5(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Sec,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange5() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange5(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "36.003µs"
	// "420.036003ms"
	// "430ms"
	// "5.155000000s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange6(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.NSec,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange6() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange6(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange7(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.USec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange7() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange7(dt)
	}

	// Output:
	// "0µs"
	// "0µs"
	// "0µs"
	// "1µs"
	// "36µs"
	// "37µs"
	// "420.037ms"
	// "430ms"
	// "5.155000s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d1h30m2s"
	// "5y314d2h"
	// "29y82d22h46m43.685478s"
	// "292y98d23h47m16.854776s"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange8(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.MSec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange8() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange8(dt)
	}

	// Output:
	// "0ms"
	// "0ms"
	// "0ms"
	// "1ms"
	// "1ms"
	// "1ms"
	// "421ms"
	// "430ms"
	// "5.155s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d1h30m2s"
	// "5y314d2h"
	// "29y82d22h46m43.686s"
	// "292y98d23h47m16.855s"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange9(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange9() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange9(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d1h30m2s"
	// "5y314d2h"
	// "29y82d22h46m44s"
	// "292y98d23h47m17s"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange10(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Min,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange10() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange10(dt)
	}

	// Output:
	// "0m"
	// "0m"
	// "0m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "2m"
	// "13m"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d1h31m"
	// "5y314d2h"
	// "29y82d22h47m"
	// "292y98d23h48m"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange11(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange11() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange11(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "3h"
	// "11h"
	// "11h"
	// "11h"
	// "12h"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "178d"
	// "178d2h"
	// "5y314d2h"
	// "29y82d23h"
	// "292y99d"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange12(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Day,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange12() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange12(dt)
	}

	// Output:
	// "0d"
	// "0d"
	// "0d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "2d"
	// "15d"
	// "15d"
	// "178d"
	// "179d"
	// "5y314d"
	// "29y83d"
	// "292y99d"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange13(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Year,
		quantity.Year,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange13() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange13(dt)
	}

	// Output:
	// "0y"
	// "0y"
	// "0y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "1y"
	// "6y"
	// "30y"
	// "293y"
	// "ages!"
	// "ages!"
}

func NoSpaceAllTimeLeftRange14(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceAllTimeLeftRange14() {

	for _, dt := range dataset {
		NoSpaceAllTimeLeftRange14(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h30m2s"
	// "11h2m"
	// "22h59m59s"
	// "23h59m59s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

// No spaces with compact packing

func NoSpaceCompTimeLeftRange1(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange1() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange1(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange2(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Day,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange2() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange2(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange3(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Hour,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange3() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange3(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange4(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Min,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange4() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange4(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange5(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.Sec,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange5() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange5(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange6(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.NSec,
		quantity.NSec,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange6() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange6(dt)
	}

	// Output:
	// "0ns"
	// "0ns"
	// "0ns"
	// "3ns"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange7(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.USec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange7() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange7(dt)
	}

	// Output:
	// "0µs"
	// "0µs"
	// "0µs"
	// "1µs"
	// "36µs"
	// "37µs"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange8(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.MSec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange8() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange8(dt)
	}

	// Output:
	// "0ms"
	// "0ms"
	// "0ms"
	// "1ms"
	// "1ms"
	// "1ms"
	// "421ms"
	// "430ms"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange9(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange9() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange9(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange10(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Min,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange10() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange10(dt)
	}

	// Output:
	// "0m"
	// "0m"
	// "0m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "1m"
	// "2m"
	// "13m"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange11(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange11() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange11(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "3h"
	// "11h"
	// "11h"
	// "11h"
	// "12h"
	// "23h"
	// "1d"
	// "1d6h"
	// "14d9h"
	// "14d21h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange12(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Day,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange12() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange12(dt)
	}

	// Output:
	// "0d"
	// "0d"
	// "0d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "1d"
	// "2d"
	// "15d"
	// "15d"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange13(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Year,
		quantity.Year,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange13() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange13(dt)
	}

	// Output:
	// "0y"
	// "0y"
	// "0y"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func NoSpaceCompTimeLeftRange14(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Sec,
		quantity.Hour,
		quantity.ShowComp,
		quantity.TimeLeft,
		false))
}

func ExampleNoSpaceCompTimeLeftRange14() {

	for _, dt := range dataset {
		NoSpaceCompTimeLeftRange14(dt)
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "6s"
	// "1m2s"
	// "12m24s"
	// "2h29m"
	// "10h9m"
	// "10h30m"
	// "10h31m"
	// "11h2m"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

// Rendering options (TimeLeft, TimePassed, TimeRounded)

func SpaceAllTimeLeft(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimeLeft,
		false))
}

func ExampleSpaceAllTimeLeft() {

	for _, dt := range dataset {
		SpaceAllTimeLeft(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "1h"
	// "3h"
	// "11h"
	// "11h"
	// "11h"
	// "12h"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimePassed(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimePassed,
		false))
}

func ExampleSpaceAllTimePassed() {

	for _, dt := range dataset {
		SpaceAllTimePassed(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "2h"
	// "10h"
	// "10h"
	// "10h"
	// "11h"
	// "22h"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

func SpaceAllTimeRounded(dt float64) {
	fmt.Printf("%q\n", quantity.FormatDurationGeneric(dt,
		quantity.Hour,
		quantity.Hour,
		quantity.ShowAll,
		quantity.TimeRounded,
		false))
}

func ExampleSpaceAllTimeRounded() {

	for _, dt := range dataset {
		SpaceAllTimeRounded(dt)
	}

	// Output:
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "0h"
	// "2h"
	// "10h"
	// "11h"
	// "11h"
	// "11h"
	// "23h"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
	// "ages!"
}

// Progress Bar Specific tests

var progress = []float64{
	time.Duration(0).Seconds(),
	time.Duration(1 * time.Nanosecond).Seconds(),
	time.Duration(400 * time.Millisecond).Seconds(),
	time.Duration(500 * time.Millisecond).Seconds(),
	time.Duration(999 * time.Millisecond).Seconds(),
	time.Duration(59*time.Second + 1*time.Nanosecond).Seconds(),
	time.Duration(59*time.Minute + 10*time.Second + 1*time.Nanosecond).Seconds(),
	time.Duration(59*time.Minute + 59*time.Second + 1*time.Nanosecond).Seconds(),
	time.Duration(22*time.Hour + 58*time.Minute + 59*time.Second).Seconds(),
	time.Duration(22*time.Hour + 59*time.Minute + 59*time.Second).Seconds(),
	time.Duration(22*time.Hour + 59*time.Minute + 59*time.Second + 1*time.Nanosecond).Seconds(),
	time.Duration(23*time.Hour + 59*time.Minute + 59*time.Second + 1*time.Nanosecond).Seconds(),
	time.Duration(48 * time.Hour).Seconds(),
}

func ExampleProgressBarTimeLeft() {

	for _, dt := range progress {
		fmt.Printf("%q\n", quantity.ProgressBarTimeLeft(dt))
	}

	// Output:
	// "0s"
	// "1s"
	// "1s"
	// "1s"
	// "1s"
	// "1m"
	// "59m11s"
	// "1h"
	// "22h59m"
	// "23h"
	// "23h"
	// "ages!"
	// "ages!"
}

func ExampleProgressBarTimePassed() {

	for _, dt := range progress {
		fmt.Printf("%q\n", quantity.ProgressBarTimePassed(dt))
	}

	// Output:
	// "0s"
	// "0s"
	// "0s"
	// "0s"
	// "0s"
	// "59s"
	// "59m10s"
	// "59m59s"
	// "22h58m"
	// "22h59m"
	// "22h59m"
	// "23h59m"
	// "ages!"
}
