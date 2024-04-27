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
	"encoding/json"
	"fmt"
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
		c.Check(err, ErrorMatches, "outcome must be .*", Commentf("data: %v", string(data)))

		var fow2 fakeOutcomeWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, prompting.OutcomeAllow, outcome))
		err = json.Unmarshal(data, &fow2)
		c.Check(err, ErrorMatches, "outcome must be .*", Commentf("data: %v", string(data)))
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
		c.Check(err, ErrorMatches, "lifespan must be .*", Commentf("data: %v", string(data)))

		var flw2 fakeLifespanWrapper
		data = []byte(fmt.Sprintf(`{"field1": "%s", "field2": "%s"}`, prompting.LifespanForever, lifespan))
		err = json.Unmarshal(data, &flw2)
		c.Check(err, ErrorMatches, "lifespan must be .*", Commentf("data: %v", string(data)))
	}
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
