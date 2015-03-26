/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/mvo5/goconfigparser"

	"launchpad.net/snappy/helpers"
)

var systemImageServer = "https://system-image.ubuntu.com/"

type updateStatus struct {
	targetVersion        string
	targetVersionDetails string
	updateSize           int64
	lastUpdate           time.Time
}

type channelImage struct {
	Descripton     string              `json:"description,omitempty"`
	Type           string              `json:"type, omitempty"`
	Version        int                 `json:"version, omitempty"`
	VersionDetails string              `json:"version_detail, omitempty"`
	Files          []channelImageFiles `json:"files"`
}

type channelImageFiles struct {
	Size int64 `json:"size"`
}

type channelImageGlobal struct {
	GeneratedAt string `json:"generated_at"`
}

type channelJSON struct {
	Global channelImageGlobal `json:"global"`
	Images []channelImage     `json:"images"`
}

func systemImageClientCheckForUpdates(configFile string) (us updateStatus, err error) {
	cfg := goconfigparser.New()
	if err := cfg.ReadFile(configFile); err != nil {
		return us, err
	}
	channel, _ := cfg.Get("service", "channel")
	device, _ := cfg.Get("service", "device")

	indexURL := systemImageServer + "/" + path.Join(channel, device, "index.json")

	resp, err := http.Get(indexURL)
	if err != nil {
		return us, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return us, fmt.Errorf("systemImageDbusProxy: unexpected http statusCode %v for %s", resp.StatusCode, indexURL)
	}

	// and decode json
	var channelData channelJSON
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&channelData); err != nil {
		return us, err
	}

	// global property
	us.lastUpdate, _ = time.Parse("Mon Jan 2 15:04:05 MST 2006", channelData.Global.GeneratedAt)

	// FIXME: find latest image of type "full" here
	latestImage := channelData.Images[len(channelData.Images)-1]
	us.targetVersion = fmt.Sprintf("%d", latestImage.Version)
	us.targetVersionDetails = latestImage.VersionDetails

	// FIXME: this is not accurate right now as it does not take
	//        the deltas into account
	for _, f := range latestImage.Files {
		us.updateSize += f.Size
	}

	return us, nil
}

type genericJSON struct {
	Type    string  `json:"type, omitempty"`
	Message string  `json:"msg, omitempty"`
	Now     float64 `json:"now, omitempty"`
	Total   float64 `json:"total, omitempty"`
}

func parseSIProgress(pb ProgressMeter, stdout io.Reader) error {
	if pb == nil {
		pb = &NullProgress{}
	}

	scanner := bufio.NewScanner(stdout)
	// s-i is funny, total changes during the runs
	total := 0.0
	pb.Start(100)

	for scanner.Scan() {
		if os.Getenv("SNAPPY_DEBUG") != "" {
			fmt.Println(scanner.Text())
		}

		jsonStream := strings.NewReader(scanner.Text())
		dec := json.NewDecoder(jsonStream)
		var genericData genericJSON
		if err := dec.Decode(&genericData); err != nil {
			// we ignore invalid json here and continue
			// the parsing if s-i-cli or ubuntu-core-upgrader
			// output something unexpected (like stray debug
			// output or whatnot)
			continue
		}

		switch {
		case genericData.Type == "spinner":
			pb.Spin(genericData.Message)
		case genericData.Type == "error":
			return fmt.Errorf("error from %s: %s", systemImageCli, genericData.Message)
		case genericData.Type == "progress":
			if total != genericData.Total {
				total = genericData.Total
				pb.SetTotal(total)
			}
			pb.Set(genericData.Now)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func systemImageDownloadUpdate(configFile string, pb ProgressMeter) (err error) {
	cmd := exec.Command(systemImageCli, "--machine-readable", "-C", configFile)

	// collect progress over stdout pipe if we want progress
	var stdout io.Reader
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return err
	}

	// collect error message (traceback etc in a separate goroutine)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stderrCh := make(chan []byte)
	go func() {
		stderrContent, _ := ioutil.ReadAll(stderr)
		stderrCh <- stderrContent
	}()

	// run it
	if err := cmd.Start(); err != nil {
		return err
	}

	// and parse progress synchronously
	if err := parseSIProgress(pb, stdout); err != nil {
		return err
	}

	// we need to read all of stderr *before* calling cmd.Wait() to avoid
	// a race, see docs for "os/exec:func (*Cmd) StdoutPipe"
	stderrContent := <-stderrCh
	if err := cmd.Wait(); err != nil {
		retCode, _ := helpers.ExitCode(err)
		return fmt.Errorf("%s failed with return code %v: %s", systemImageCli, retCode, string(stderrContent))
	}

	return err
}
