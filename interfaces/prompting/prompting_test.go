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

package prompting_test

import (
	"encoding/base32"
	"encoding/binary"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
)

func Test(t *testing.T) { TestingT(t) }

type promptingSuite struct {
	tmpdir string
}

var _ = Suite(&promptingSuite{})

func (s *promptingSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *promptingSuite) TestOutcomeIsAllow(c *C) {
	result, err := prompting.OutcomeAllow.IsAllow()
	c.Check(err, IsNil)
	c.Check(result, Equals, true)
	result, err = prompting.OutcomeDeny.IsAllow()
	c.Check(err, IsNil)
	c.Check(result, Equals, false)
	_, err = prompting.OutcomeUnset.IsAllow()
	c.Check(err, ErrorMatches, `outcome must be.*`)
	_, err = prompting.OutcomeType("foo").IsAllow()
	c.Check(err, ErrorMatches, `outcome must be.*`)
}

func (s *promptingSuite) TestValidateOutcome(c *C) {
	c.Assert(prompting.ValidateOutcome(prompting.OutcomeAllow), Equals, nil)
	c.Assert(prompting.ValidateOutcome(prompting.OutcomeDeny), Equals, nil)
	c.Assert(prompting.ValidateOutcome(prompting.OutcomeUnset), ErrorMatches, `outcome must be.*`)
	c.Assert(prompting.ValidateOutcome(prompting.OutcomeType("foo")), ErrorMatches, `outcome must be.*`)
}

func (s *promptingSuite) TestValidateLifespanExpiration(c *C) {
	var unsetExpiration time.Time
	currTime := time.Now()
	negativeExpiration := currTime.Add(-5 * time.Second)
	validExpiration := currTime.Add(10 * time.Minute)

	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
	} {
		err := prompting.ValidateLifespanExpiration(lifespan, unsetExpiration, currTime)
		c.Check(err, IsNil)
		for _, exp := range []time.Time{negativeExpiration, validExpiration} {
			err = prompting.ValidateLifespanExpiration(lifespan, exp, currTime)
			c.Check(err, ErrorMatches, `expiration must be omitted.*`)
		}
	}

	err := prompting.ValidateLifespanExpiration(prompting.LifespanTimespan, unsetExpiration, currTime)
	c.Check(err, ErrorMatches, `expiration must be non-zero.*`)

	err = prompting.ValidateLifespanExpiration(prompting.LifespanTimespan, negativeExpiration, currTime)
	c.Check(err, ErrorMatches, `expiration time has already passed.*`)

	err = prompting.ValidateLifespanExpiration(prompting.LifespanTimespan, validExpiration, currTime)
	c.Check(err, IsNil)
}

func (s *promptingSuite) TestValidateLifespanParseDuration(c *C) {
	unsetDuration := ""
	invalidDuration := "foo"
	negativeDuration := "-5s"
	validDuration := "10m"
	parsedValidDuration, err := time.ParseDuration(validDuration)
	c.Assert(err, IsNil)

	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
	} {
		expiration, err := prompting.ValidateLifespanParseDuration(lifespan, unsetDuration)
		c.Check(expiration.IsZero(), Equals, true)
		c.Check(err, IsNil)
		for _, dur := range []string{invalidDuration, negativeDuration, validDuration} {
			expiration, err = prompting.ValidateLifespanParseDuration(lifespan, dur)
			c.Check(expiration.IsZero(), Equals, true)
			c.Check(err, ErrorMatches, `duration must be empty.*`)
		}
	}

	expiration, err := prompting.ValidateLifespanParseDuration(prompting.LifespanTimespan, unsetDuration)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `duration must be non-empty.*`)

	expiration, err = prompting.ValidateLifespanParseDuration(prompting.LifespanTimespan, invalidDuration)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `error parsing duration string.*`)

	expiration, err = prompting.ValidateLifespanParseDuration(prompting.LifespanTimespan, negativeDuration)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `duration must be greater than zero.*`)

	expiration, err = prompting.ValidateLifespanParseDuration(prompting.LifespanTimespan, validDuration)
	c.Check(err, IsNil)
	c.Check(expiration.After(time.Now()), Equals, true)
	c.Check(expiration.Before(time.Now().Add(parsedValidDuration)), Equals, true)
}

func (s *promptingSuite) TestNewIDAndTimestamp(c *C) {
	before := time.Now()
	id, _ := prompting.NewIDAndTimestamp()
	idPaired, timestampPaired := prompting.NewIDAndTimestamp()
	after := time.Now()
	data1, err := base32.StdEncoding.DecodeString(id)
	c.Assert(err, IsNil)
	data2, err := base32.StdEncoding.DecodeString(idPaired)
	c.Assert(err, IsNil)
	parsedNs := int64(binary.BigEndian.Uint64(data1))
	parsedNsPaired := int64(binary.BigEndian.Uint64(data2))
	parsedTime := time.Unix(parsedNs/1000000000, parsedNs%1000000000)
	parsedTimePaired := time.Unix(parsedNsPaired/1000000000, parsedNsPaired%1000000000)
	c.Assert(parsedTime.After(before), Equals, true)
	c.Assert(parsedTime.Before(after), Equals, true)
	c.Assert(parsedTimePaired.After(before), Equals, true)
	c.Assert(parsedTimePaired.Before(after), Equals, true)
	c.Assert(parsedTimePaired.Equal(timestampPaired), Equals, true)
}
