// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/template"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

var assetTemplateText = `// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package assets

// Code generated from {{ .InputFileName }} DO NOT EDIT

func init() {
	registerInternal("{{ .AssetName }}", []byte{
{{ range .AssetDataLines }}		{{ . }}
{{ end }}	})
}
`

var (
	inFile        = flag.String("in", "", "asset input file")
	outFile       = flag.String("out", "", "asset output file")
	assetName     = flag.String("name", "", "asset name")
	assetTemplate = template.Must(template.New("asset").Parse(assetTemplateText))
)

// formatLines generates a list of strings, each carrying a line containing hex
// encoded data
func formatLines(data []byte) []string {
	const lineBreak = 16

	l := len(data)
	lines := make([]string, 0, l/lineBreak+1)
	for i := 0; i < l; i = i + lineBreak {
		end := i + lineBreak
		start := i
		if end > l {
			end = l
		}
		var line bytes.Buffer
		forLine := data[start:end]
		for idx, val := range forLine {
			line.WriteString(fmt.Sprintf("0x%02x,", val))
			if idx != len(forLine)-1 {
				line.WriteRune(' ')
			}
		}
		lines = append(lines, line.String())
	}
	return lines
}

func run(assetName, inputFile, outputFile string) error {
	inf := mylog.Check2(os.Open(inputFile))

	defer inf.Close()

	var inData bytes.Buffer
	mylog.Check2(io.Copy(&inData, inf))

	outf := mylog.Check2(osutil.NewAtomicFile(outputFile, 0644, 0, osutil.NoChown, osutil.NoChown))

	defer outf.Cancel()

	templateData := struct {
		Comment        string
		InputFileName  string
		AssetName      string
		AssetDataLines []string
		Year           string
	}{
		InputFileName: inputFile,
		// dealing with precise formatting in template is annoying thus
		// we use a preformatted lines carrying asset data
		AssetDataLines: formatLines(inData.Bytes()),
		AssetName:      assetName,
		// XXX: The year is currently not used because it leads
		//      to spurious changes every year. Once we use something
		//      like real build-system we can re-enable this
		Year: strconv.Itoa(time.Now().Year()),
	}
	mylog.Check(assetTemplate.Execute(outf, &templateData))

	return outf.Commit()
}

func parseArgs() error {
	flag.Parse()
	if *inFile == "" {
		return fmt.Errorf("input file not provided")
	}
	if *outFile == "" {
		return fmt.Errorf("output file not provided")
	}
	if *assetName == "" {
		return fmt.Errorf("asset name not provided")
	}
	return nil
}

func main() {
	mylog.Check(parseArgs())
	mylog.Check(run(*assetName, *inFile, *outFile))
}
