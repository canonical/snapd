// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

// make sure that there are no collisions
const (
	port    = "9999"
	baseURL = "http://127.0.0.1:" + port
)

var _ = check.Suite(&snapdTestSuite{})

type snapdTestSuite struct {
	common.SnappySuite
	cmd *exec.Cmd
}

func (s *snapdTestSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)
	s.cmd = exec.Command("sudo", "env", "PATH="+os.Getenv("PATH"),
		"/lib/systemd/systemd-activate",
		"-l", "0.0.0.0:"+port, "snapd")

	s.cmd.Start()

	intPort, _ := strconv.Atoi(port)
	err := wait.ForServerOnPort(c, "tcp", intPort)
	c.Assert(err, check.IsNil)
}

func (s *snapdTestSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)

	proc := s.cmd.Process
	if proc != nil {
		proc.Kill()
	}
}

type response struct {
	Result     interface{}
	Status     string
	Type       string
	StatusCode int `json:"status_code"`
}

type asyncResponse struct {
	Result asyncResult
	response
}

type asyncResult struct {
	CreatedAt string `json:"created_at"`
	MayCancel bool   `json:"may_cancel"`
	Output    string
	Resource  string
	Status    string
	UpdatedAt string `json:"updated_at"`
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
	// returns the path of the resource to be tested, as in "/1.0/packages"
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

// all the interactions are dispatched with this function
func doInteraction(c *check.C, resource, verb string, interaction apiInteraction) {
	log.Printf("** Trying interaction %v", interaction)

	var (
		payload io.Reader
		err     error
	)
	if _, err = os.Stat(interaction.payload); os.IsNotExist(err) {
		// The payload is not a file. Treat it as a string
		payload = strings.NewReader(interaction.payload)
	} else {
		payload, err = os.Open(interaction.payload)
		f, _ := payload.(*os.File)
		defer f.Close()
	}

	body, err := genericRequest(resource, verb, payload)
	c.Check(err, check.IsNil, check.Commentf("Error making the request: %s", err))

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
	doInteraction(c,
		resource+"/not-a-resource",
		"GET",
		apiInteraction{
			responsePattern: `{"result":{},"status":"Not Found","status_code":404,"type":"error"}`})
}

func doMethodNotAllowed(c *check.C, resource, verb string) {
	doInteraction(c,
		resource,
		verb,
		apiInteraction{
			responsePattern: `{"result":{},"status":"Unauthorized","status_code":401,"type":"error"}`})
}

// this is the function which makes requests to the api
func genericRequest(resource, verb string, payload io.Reader) (body []byte, err error) {
	if file, ok := payload.(*os.File); ok {
		defer file.Close()
	}

	req, err := http.NewRequest(verb, resource, payload)
	if err != nil {
		return
	}
	req.Header.Add("X-Allow-Unsigned", "1")

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return
	}
	return ioutil.ReadAll(resp.Body)
}
