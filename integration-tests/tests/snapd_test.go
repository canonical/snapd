// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package tests

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

const (
	baseURL        = "snapd://"
	httpClientSnap = "http.chipaca"
)

var _ = check.Suite(&snapdTestSuite{})

type snapdTestSuite struct {
	common.SnappySuite
}

func (s *snapdTestSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	c.Skip("FIXME: we need to update http.chipaca to new-security *and* land  auto-connect support in snapd")

	common.InstallSnap(c, httpClientSnap+"/edge")
}

func (s *snapdTestSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)

	common.RemoveSnap(c, httpClientSnap)
}

type response struct {
	Result     interface{}
	Status     string
	Type       string
	StatusCode int `json:"status-code"`
}

type asyncResponse struct {
	Result asyncResult
	response
}

type asyncResult struct {
	CreatedAt string `json:"created-at"`
	MayCancel bool   `json:"may-cancel"`
	Output    string
	Resource  string
	Status    string
	UpdatedAt string `json:"updated-at"`
}

// type for describing interactions with the api
type apiInteraction struct {
	// payload can describe a file path or be the content to be appended to the
	// body of the request
	payload string
	// expected pattern for the response
	responsePattern string
	// expected structure of the response
	responseObject interface{}
	// for async request, the test will keep executing the waitFunction
	// until the waitPattern is received or a timeout expires
	waitPattern  string
	waitFunction func() (string, error)
}

type apiInteractions []apiInteraction

// all the api test suites must satisfy this interface
type apiExerciser interface {
	// returns the path of the resource to be tested, as in "/v2/snaps"
	resource() string
}

// concrete interface for apis exercising GET
type apiGetExerciser interface {
	getInteractions() apiInteractions
}

// concrete interface for apis exercising POST
type apiPostExerciser interface {
	postInteractions() apiInteractions
}

// concrete interface for apis exercising PUT
type apiPutExerciser interface {
	putInteractions() apiInteractions
}

// concrete interface for apis exercising DELETE
type apiDeleteExerciser interface {
	deleteInteractions() apiInteractions
}

// options passed to the request method
type requestOptions struct {
	resource, verb, payload string
}

// this is the entry point for all the api tests
func exerciseAPI(c *check.C, a apiExerciser) {
	resource := a.resource()

	do404(c, resource)

	if getInstance, ok := a.(apiGetExerciser); ok {
		doInteractions(c, resource, "GET", getInstance.getInteractions())
	} else {
		doMethodNotAllowed(c, resource, "GET")
	}

	if postInstance, ok := a.(apiPostExerciser); ok {
		doInteractions(c, resource, "POST", postInstance.postInteractions())
	} else {
		doMethodNotAllowed(c, resource, "POST")
	}

	if putInstance, ok := a.(apiPutExerciser); ok {
		doInteractions(c, resource, "PUT", putInstance.putInteractions())
	} else {
		doMethodNotAllowed(c, resource, "PUT")
	}

	if deleteInstance, ok := a.(apiDeleteExerciser); ok {
		doInteractions(c, resource, "DELETE", deleteInstance.deleteInteractions())
	} else {
		doMethodNotAllowed(c, resource, "DELETE")
	}
}

func doInteractions(c *check.C, resource, verb string, interactions apiInteractions) {
	log.Printf("*** Exercising API for resource %s with verb %s", resource, verb)

	for _, interaction := range interactions {
		doInteraction(c, resource, verb, interaction)
	}
}

// doInteraction dispatches the given interaction to the resource using the provided verb
func doInteraction(c *check.C, resource, verb string, interaction apiInteraction) {
	log.Printf("** Trying interaction %v", interaction)

	body, err := makeRequest(&requestOptions{resource: resource, verb: verb, payload: interaction.payload})
	c.Assert(err, check.IsNil, check.Commentf("Error making the request: %s", err))

	if interaction.responseObject == nil {
		interaction.responseObject = &response{}
	}
	err = json.Unmarshal(body, interaction.responseObject)
	c.Check(err, check.IsNil, check.Commentf("Error unmarshalling the response: %s", err))

	if interaction.responsePattern != "" {
		c.Check(string(body), check.Matches, interaction.responsePattern)
	}
	if interaction.waitPattern != "" {
		err = wait.ForFunction(c, interaction.waitPattern, interaction.waitFunction)
		c.Check(err, check.IsNil, check.Commentf("Error waiting for function: %s", err))
	}
}

func do404(c *check.C, resource string) {
	path := "not-a-resource"
	if resource[len(resource)-1:] != "/" {
		path = "/" + path
	}
	doInteraction(c,
		resource+path,
		"GET",
		apiInteraction{
			responsePattern: `{"result":{"message":".*"},"status":"Not Found","status-code":404,"type":"error"}`})
}

func doMethodNotAllowed(c *check.C, resource, verb string) {
	doInteraction(c,
		resource,
		verb,
		apiInteraction{
			responsePattern: `{"result":{"message":"method \S+ not allowed"},"status":"Method Not Allowed","status-code":405,"type":"error"}`})
}

// makeRequest makes a request to the API according to the provided options.
func makeRequest(options *requestOptions) (body []byte, err error) {
	cmd := []string{"sudo", "http.do",
		"--pretty", "none",
		"--body",
		"--ignore-stdin",
		options.verb, options.resource}

	if options.payload != "" {
		payload, err := determinePayload(options.payload)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(payload, "@") {
			defer os.Remove(payload[1:])
		}
		cmd = append(cmd, payload)
	}

	bodyString, err := cli.ExecCommandErr(append(cmd, "X-Allow-Unsigned:1")...)

	return []byte(bodyString), err
}

func determinePayload(payload string) (string, error) {
	if _, err := os.Stat(payload); err == nil {
		// payload is a file, in order to make the snap file available to http we need to move it to its $SNAP_DATA path
		snapAppDataPath := filepath.Join("/var/lib/snapd", strings.Split(httpClientSnap, ".")[0], "current")
		if _, err := cli.ExecCommandErr("sudo", "cp", payload, snapAppDataPath); err != nil {
			return "", err
		}
		return "@" + filepath.Join(snapAppDataPath, filepath.Base(payload)), nil
	}
	// payload is a string
	return payload, nil
}
