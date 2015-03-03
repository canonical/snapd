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

func systemImageDownloadUpdate(configFile string, pb ProgressMeter) (err error) {
	cmd := exec.Command(systemImageCli, "--machine-readable", "-C", configFile)

	// collect progress over stdout
	var stdout io.Reader
	if pb != nil {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
	}

	// collect error message (traceback etc)
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

	// and parse progress
	if pb != nil {
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
	}

	if err := cmd.Wait(); err != nil {
		stderrContent := <-stderrCh
		retCode, _ := helpers.ExitCode(err)
		return fmt.Errorf("%s failed with return code %v: %s", systemImageCli, retCode, string(stderrContent))
	}

	return err
}
