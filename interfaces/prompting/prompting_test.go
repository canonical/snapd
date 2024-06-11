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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting"
)

func Test(t *testing.T) { TestingT(t) }

type promptingSuite struct{}

var _ = Suite(&promptingSuite{})

func (s *promptingSuite) TestOutcomeAsBool(c *C) {
	result, err := prompting.OutcomeAllow.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, true)
	result, err = prompting.OutcomeDeny.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, false)
	_, err = prompting.OutcomeUnset.AsBool()
	c.Check(err, ErrorMatches, `internal error: invalid outcome.*`)
	_, err = prompting.OutcomeType("foo").AsBool()
	c.Check(err, ErrorMatches, `internal error: invalid outcome.*`)
}

type fakeOutcomeWrapper struct {
	Field1 prompting.OutcomeType `json:"field1"`
	Field2 prompting.OutcomeType `json:"field2,omitempty"`
}

func (s *promptingSuite) TestUnmarshalOutcomeHappy(c *C) {
	for _, outcome := range []prompting.OutcomeType{
		prompting.OutcomeAllow,
		prompting.OutcomeDeny,
	} {
		var fow1 fakeOutcomeWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, outcome, outcome))
		err := json.Unmarshal(data, &fow1)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(fow1.Field1, Equals, outcome, Commentf("data: %v", string(data)))
		c.Check(fow1.Field2, Equals, outcome, Commentf("data: %v", string(data)))

		var fow2 fakeOutcomeWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s"}`, outcome))
		err = json.Unmarshal(data, &fow2)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(fow2.Field1, Equals, outcome, Commentf("data: %v", string(data)))
		c.Check(fow2.Field2, Equals, prompting.OutcomeUnset, Commentf("data: %v", string(data)))
	}
}

func (s *promptingSuite) TestUnmarshalOutcomeUnhappy(c *C) {
	for _, outcome := range []prompting.OutcomeType{
		prompting.OutcomeUnset,
		prompting.OutcomeType("foo"),
	} {
		var fow1 fakeOutcomeWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, outcome, outcome))
		err := json.Unmarshal(data, &fow1)
		c.Check(err, ErrorMatches, `cannot have outcome other than.*`, Commentf("data: %v", string(data)))

		var fow2 fakeOutcomeWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, prompting.OutcomeAllow, outcome))
		err = json.Unmarshal(data, &fow2)
		c.Check(err, ErrorMatches, `cannot have outcome other than.*`, Commentf("data: %v", string(data)))
	}
}

type fakeLifespanWrapper struct {
	Field1 prompting.LifespanType `json:"field1"`
	Field2 prompting.LifespanType `json:"field2,omitempty"`
}

func (s *promptingSuite) TestUnmarshalLifespanHappy(c *C) {
	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
		prompting.LifespanTimespan,
	} {
		var flw1 fakeLifespanWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, lifespan, lifespan))
		err := json.Unmarshal(data, &flw1)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(flw1.Field1, Equals, lifespan, Commentf("data: %v", string(data)))
		c.Check(flw1.Field2, Equals, lifespan, Commentf("data: %v", string(data)))

		var flw2 fakeLifespanWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s"}`, lifespan))
		err = json.Unmarshal(data, &flw2)
		c.Check(err, IsNil, Commentf("data: %v", string(data)))
		c.Check(flw2.Field1, Equals, lifespan, Commentf("data: %v", string(data)))
		c.Check(flw2.Field2, Equals, prompting.LifespanUnset, Commentf("data: %v", string(data)))
	}
}

func (s *promptingSuite) TestUnmarshalLifespanUnhappy(c *C) {
	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanUnset,
		prompting.LifespanType("foo"),
	} {
		var flw1 fakeLifespanWrapper
		data := []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, lifespan, lifespan))
		err := json.Unmarshal(data, &flw1)
		c.Check(err, ErrorMatches, `cannot have lifespan other than.*`, Commentf("data: %v", string(data)))

		var flw2 fakeLifespanWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, prompting.LifespanForever, lifespan))
		err = json.Unmarshal(data, &flw2)
		c.Check(err, ErrorMatches, `cannot have lifespan other than.*`, Commentf("data: %v", string(data)))
	}
}

func (s *promptingSuite) TestValidateExpiration(c *C) {
	var unsetExpiration time.Time
	currTime := time.Now()
	negativeExpiration := currTime.Add(-5 * time.Second)
	validExpiration := currTime.Add(10 * time.Minute)

	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
	} {
		err := lifespan.ValidateExpiration(unsetExpiration, currTime)
		c.Check(err, IsNil)
		for _, exp := range []time.Time{negativeExpiration, validExpiration} {
			err = lifespan.ValidateExpiration(exp, currTime)
			c.Check(err, ErrorMatches, `cannot have specified expiration when lifespan is.*`)
		}
	}

	err := prompting.LifespanTimespan.ValidateExpiration(unsetExpiration, currTime)
	c.Check(err, ErrorMatches, `cannot have unspecified expiration when lifespan is.*`)

	err = prompting.LifespanTimespan.ValidateExpiration(negativeExpiration, currTime)
	c.Check(err, ErrorMatches, `cannot have expiration time in the past.*`)

	err = prompting.LifespanTimespan.ValidateExpiration(validExpiration, currTime)
	c.Check(err, IsNil)
}

func (s *promptingSuite) TestParseDuration(c *C) {
	unsetDuration := ""
	invalidDuration := "foo"
	negativeDuration := "-5s"
	validDuration := "10m"
	parsedValidDuration, err := time.ParseDuration(validDuration)
	c.Assert(err, IsNil)
	currTime := time.Now()

	for _, lifespan := range []prompting.LifespanType{
		prompting.LifespanForever,
		prompting.LifespanSingle,
	} {
		expiration, err := lifespan.ParseDuration(unsetDuration, currTime)
		c.Check(expiration.IsZero(), Equals, true)
		c.Check(err, IsNil)
		for _, dur := range []string{invalidDuration, negativeDuration, validDuration} {
			expiration, err = lifespan.ParseDuration(dur, currTime)
			c.Check(expiration.IsZero(), Equals, true)
			c.Check(err, ErrorMatches, `cannot have specified duration when lifespan is.*`)
		}
	}

	expiration, err := prompting.LifespanTimespan.ParseDuration(unsetDuration, currTime)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `cannot have unspecified duration when lifespan is.*`)

	expiration, err = prompting.LifespanTimespan.ParseDuration(invalidDuration, currTime)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `cannot parse duration.*`)

	expiration, err = prompting.LifespanTimespan.ParseDuration(negativeDuration, currTime)
	c.Check(expiration.IsZero(), Equals, true)
	c.Check(err, ErrorMatches, `cannot have zero or negative duration.*`)

	expiration, err = prompting.LifespanTimespan.ParseDuration(validDuration, currTime)
	c.Check(err, IsNil)
	c.Check(expiration.After(time.Now()), Equals, true)
	c.Check(expiration.Before(time.Now().Add(parsedValidDuration)), Equals, true)

	expiration2, err := prompting.LifespanTimespan.ParseDuration(validDuration, currTime)
	c.Check(err, IsNil)
	c.Check(expiration2.Equal(expiration), Equals, true)
}
