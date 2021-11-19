// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

type quantitySuite struct{}

var _ = Suite(&quantitySuite{})

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

func (s *quantitySuite) TestInv(c *C) {
	for _, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Year,
			quantity.Second,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			true))

		c.Check(output, Equals, "inv!")
	}

	output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(1,
		quantity.Second,
		quantity.Year,
		quantity.ShowVerbose,
		5,
		true))

	c.Check(output, Equals, "inv!")
}

// Spaces with verbose packing

func (s *quantitySuite) TestSpaceAllTimeLeftRange1(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 1h 30m 2s",
		"5y 314d 2h",
		"29y 82d 22h 46m 43.685477632s",
		"292y 98d 23h 47m 16.854775808s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange2(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 1h 30m 2s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Day,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange3(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange4(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m 2s",
		"12m 24s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Minute,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange5(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Second,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange6(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.NSecond,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange7(c *C) {
	expected := []string{
		"0µs",
		"0µs",
		"0µs",
		"1µs",
		"36µs",
		"37µs",
		"420.037ms",
		"430ms",
		"5.155000s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 1h 30m 2s",
		"5y 314d 2h",
		"29y 82d 22h 46m 43.685478s",
		"292y 98d 23h 47m 16.854776s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.USecond,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange8(c *C) {
	expected := []string{
		"0ms",
		"0ms",
		"0ms",
		"1ms",
		"1ms",
		"1ms",
		"421ms",
		"430ms",
		"5.155s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 1h 30m 2s",
		"5y 314d 2h",
		"29y 82d 22h 46m 43.686s",
		"292y 98d 23h 47m 16.855s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.MSecond,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange9(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 1h 30m 2s",
		"5y 314d 2h",
		"29y 82d 22h 46m 44s",
		"292y 98d 23h 47m 17s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange10(c *C) {
	expected := []string{
		"0m",
		"0m",
		"0m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"2m",
		"13m",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 1h 31m",
		"5y 314d 2h",
		"29y 82d 22h 47m",
		"292y 98d 23h 48m",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Minute,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange11(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"3h",
		"11h",
		"11h",
		"11h",
		"12h",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"178d",
		"178d 2h",
		"5y 314d 2h",
		"29y 82d 23h",
		"292y 99d",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange12(c *C) {
	expected := []string{
		"0d",
		"0d",
		"0d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"2d",
		"15d",
		"15d",
		"178d",
		"179d",
		"5y 314d",
		"29y 83d",
		"292y 99d",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Day,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange13(c *C) {
	expected := []string{
		"0y",
		"0y",
		"0y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"6y",
		"30y",
		"293y",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Year,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeLeftRange14(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 30m 2s",
		"11h 2m",
		"22h 59m 59s",
		"23h 59m 59s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

// Spaces with compact packing

func (s *quantitySuite) TestSpaceCompTimeLeftRange1(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange2(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Day,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange3(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Hour,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange4(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m 2s",
		"12m 24s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Minute,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange5(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Second,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange6(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.NSecond,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange7(c *C) {
	expected := []string{
		"0µs",
		"0µs",
		"0µs",
		"1µs",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.USecond,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange8(c *C) {
	expected := []string{
		"0ms",
		"0ms",
		"0ms",
		"1ms",
		"1ms",
		"1ms",
		"421ms",
		"430ms",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.MSecond,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange9(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange10(c *C) {
	expected := []string{
		"0m",
		"0m",
		"0m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"2m",
		"13m",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Minute,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange11(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"3h",
		"11h",
		"11h",
		"11h",
		"12h",
		"23h",
		"1d",
		"1d 6h",
		"14d 9h",
		"14d 21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange12(c *C) {
	expected := []string{
		"0d",
		"0d",
		"0d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"2d",
		"15d",
		"15d",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Day,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange13(c *C) {
	expected := []string{
		"0y",
		"0y",
		"0y",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Year,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceCompTimeLeftRange14(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m 2s",
		"12m 24s",
		"2h 29m",
		"10h 9m",
		"10h 30m",
		"10h 31m",
		"11h 2m",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Hour,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOn))

		c.Check(output, Equals, expected[i])
	}
}

// No spaces with verbose packing

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange1(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d1h30m2s",
		"5y314d2h",
		"29y82d22h46m43.685477632s",
		"292y98d23h47m16.854775808s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange2(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d1h30m2s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Day,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange3(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange4(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"1m2s",
		"12m24s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Minute,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange5(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"36.003µs",
		"420.036003ms",
		"430ms",
		"5.155000000s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Second,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange6(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.NSecond,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange7(c *C) {
	expected := []string{
		"0µs",
		"0µs",
		"0µs",
		"1µs",
		"36µs",
		"37µs",
		"420.037ms",
		"430ms",
		"5.155000s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d1h30m2s",
		"5y314d2h",
		"29y82d22h46m43.685478s",
		"292y98d23h47m16.854776s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.USecond,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange8(c *C) {
	expected := []string{
		"0ms",
		"0ms",
		"0ms",
		"1ms",
		"1ms",
		"1ms",
		"421ms",
		"430ms",
		"5.155s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d1h30m2s",
		"5y314d2h",
		"29y82d22h46m43.686s",
		"292y98d23h47m16.855s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.MSecond,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange9(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d1h30m2s",
		"5y314d2h",
		"29y82d22h46m44s",
		"292y98d23h47m17s",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange10(c *C) {
	expected := []string{
		"0m",
		"0m",
		"0m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"2m",
		"13m",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d1h31m",
		"5y314d2h",
		"29y82d22h47m",
		"292y98d23h48m",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Minute,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange11(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"3h",
		"11h",
		"11h",
		"11h",
		"12h",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"178d",
		"178d2h",
		"5y314d2h",
		"29y82d23h",
		"292y99d",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange12(c *C) {
	expected := []string{
		"0d",
		"0d",
		"0d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"2d",
		"15d",
		"15d",
		"178d",
		"179d",
		"5y314d",
		"29y83d",
		"292y99d",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Day,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange13(c *C) {
	expected := []string{
		"0y",
		"0y",
		"0y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"1y",
		"6y",
		"30y",
		"293y",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Year,
			quantity.Year,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceAllTimeLeftRange14(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h30m2s",
		"11h2m",
		"22h59m59s",
		"23h59m59s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

// No spaces with compact packing

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange1(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange2(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Day,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange3(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Hour,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange4(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m2s",
		"12m24s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Minute,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange5(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.Second,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange6(c *C) {
	expected := []string{
		"0ns",
		"0ns",
		"0ns",
		"3ns",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.NSecond,
			quantity.NSecond,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange7(c *C) {
	expected := []string{
		"0µs",
		"0µs",
		"0µs",
		"1µs",
		"36µs",
		"37µs",
		"421ms",
		"430ms",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.USecond,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange8(c *C) {
	expected := []string{
		"0ms",
		"0ms",
		"0ms",
		"1ms",
		"1ms",
		"1ms",
		"421ms",
		"430ms",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.MSecond,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange9(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange10(c *C) {
	expected := []string{
		"0m",
		"0m",
		"0m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"1m",
		"2m",
		"13m",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Minute,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange11(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"3h",
		"11h",
		"11h",
		"11h",
		"12h",
		"23h",
		"1d",
		"1d6h",
		"14d9h",
		"14d21h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange12(c *C) {
	expected := []string{
		"0d",
		"0d",
		"0d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"1d",
		"2d",
		"15d",
		"15d",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Day,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange13(c *C) {
	expected := []string{
		"0y",
		"0y",
		"0y",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Year,
			quantity.Year,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestNoSpaceCompTimeLeftRange14(c *C) {
	expected := []string{
		"0s",
		"0s",
		"0s",
		"1s",
		"1s",
		"1s",
		"1s",
		"1s",
		"6s",
		"1m2s",
		"12m24s",
		"2h29m",
		"10h9m",
		"10h30m",
		"10h31m",
		"11h2m",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Second,
			quantity.Hour,
			quantity.ShowCompact,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

// Rendering options (TimeLeft, TimePassed, TimeRounded)

func (s *quantitySuite) TestSpaceAllTimeLeft(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"1h",
		"3h",
		"11h",
		"11h",
		"11h",
		"12h",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimeLeft,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimePassed(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"2h",
		"10h",
		"10h",
		"10h",
		"11h",
		"22h",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimePassed,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}

func (s *quantitySuite) TestSpaceAllTimeRounded(c *C) {
	expected := []string{
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"0h",
		"2h",
		"10h",
		"11h",
		"11h",
		"11h",
		"23h",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
		"ages!",
	}
	for i, dt := range dataset {
		output := fmt.Sprintf("%s", quantity.FormatDurationGeneric(dt,
			quantity.Hour,
			quantity.Hour,
			quantity.ShowVerbose,
			quantity.TimeRounded,
			quantity.SpaceOff))

		c.Check(output, Equals, expected[i])
	}
}
