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

package tpl

import (
	"io/ioutil"
	"os"
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type TemplateSuite struct{}

var _ = check.Suite(&TemplateSuite{})

func (s *TemplateSuite) TestExecute(c *check.C) {
	templateContents := "bla bla {{ .Par1 }} blabla {{ .Par2 }} blaaa"
	templateFile, _ := ioutil.TempFile("", "snappy-tpl-test")
	err := ioutil.WriteFile(templateFile.Name(), []byte(templateContents), 0644)
	c.Assert(err, check.IsNil, check.Commentf("Error writing test template file"))
	defer os.Remove(templateFile.Name())

	outputFile, _ := ioutil.TempFile("", "snappy-tpl-test-output")
	data := struct{ Par1, Par2 string }{"mypar1", "mypar2"}

	err = Execute(templateFile.Name(), outputFile.Name(), data)
	defer os.Remove(outputFile.Name())
	c.Assert(err, check.IsNil, check.Commentf("Error while creating file from template"))

	outputContents, err := ioutil.ReadFile(outputFile.Name())
	c.Assert(err, check.IsNil,
		check.Commentf("Error while reading output file from template execution"))
	expectedContents := "bla bla mypar1 blabla mypar2 blaaa"

	c.Assert(string(outputContents), check.Equals, expectedContents,
		check.Commentf(
			"The parsed template contents do not match the expected contents"))
}

func (s *TemplateSuite) TestErrorExecute(c *check.C) {
	templateContents := "bla bla {{ .Par1 }} blabla {{ .Par2 }} blaaa"
	templateFile, _ := ioutil.TempFile("", "snappy-tpl-test")
	err := ioutil.WriteFile(templateFile.Name(), []byte(templateContents), 0644)
	c.Assert(err, check.IsNil, check.Commentf("Error writing test template file"))
	defer os.Remove(templateFile.Name())

	outputFile, _ := ioutil.TempFile("", "snappy-tpl-test-output")
	data := struct{ Par3, Par4 string }{"mypar1", "mypar2"}

	err = Execute(templateFile.Name(), outputFile.Name(), data)
	defer os.Remove(outputFile.Name())
	c.Assert(err, check.NotNil, check.Commentf("Expected error while executing template not received"))
}

func (s *TemplateSuite) TestErrorTemplateParse(c *check.C) {
	wrongTemplateContents := "bla bla {{ .Par1 }} blabla {{ Par2 } blaaa"
	templateFile, _ := ioutil.TempFile("", "snappy-tpl-test")
	err := ioutil.WriteFile(templateFile.Name(), []byte(wrongTemplateContents), 0644)
	c.Assert(err, check.IsNil, check.Commentf("Error writing test template file"))
	defer os.Remove(templateFile.Name())

	outputFile, _ := ioutil.TempFile("", "snappy-tpl-test-output")
	data := struct{ Par1, Par2 string }{"mypar1", "mypar2"}

	err = Execute(templateFile.Name(), outputFile.Name(), data)
	defer os.Remove(outputFile.Name())
	c.Assert(err, check.NotNil, check.Commentf("Expected error while executing template not received"))
}

func (s *TemplateSuite) TestErrorOutputFileCreate(c *check.C) {
	templateContents := "bla bla {{ .Par1 }} blabla {{ .Par2 }} blaaa"
	templateFile, _ := ioutil.TempFile("", "snappy-tpl-test")
	err := ioutil.WriteFile(templateFile.Name(), []byte(templateContents), 0644)
	c.Assert(err, check.IsNil, check.Commentf("Error writing test template file"))
	defer os.Remove(templateFile.Name())

	wrongOutputFile := "/unexistent/path/with/unexistent/file"
	data := struct{ Par1, Par2 string }{"mypar1", "mypar2"}

	err = Execute(templateFile.Name(), wrongOutputFile, data)
	c.Assert(err, check.NotNil, check.Commentf("Expected error while executing template not received"))
}
