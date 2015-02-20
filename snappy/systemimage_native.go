package snappy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/mvo5/goconfigparser"
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

type progressJSON struct {
	Now   float64
	Total float64
}

type spinnerJSON struct {
	Message string `json:"msg, omitempty"`
}

func systemImageDownloadUpdate(configFile string, pb ProgressMeter) (err error) {
	cmd := exec.Command(systemImageCli, "--machine-readable", "-C", configFile)

	var stdout io.Reader
	if pb != nil {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return err
		}
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	if pb != nil {
		scanner := bufio.NewScanner(stdout)
		// s-i is funny, total changes
		total := 0.0

		for scanner.Scan() {
			if os.Getenv("SNAPPY_DEBUG") != "" {
				fmt.Println(scanner.Text())
			}

			l := strings.SplitN(scanner.Text(), ":", 2)
			// invalid line, ignore
			if len(l) != 2 {
				continue
			}
			key := l[0]
			jsonStream := strings.NewReader(l[1])
			switch {
			case key == "PROGRESS":
				var progressData progressJSON
				dec := json.NewDecoder(jsonStream)
				if err := dec.Decode(&progressData); err != nil {
					continue
				}
				if total != progressData.Total {
					total = progressData.Total
					pb.Start(total)
				}
				pb.Set(progressData.Now)
			case key == "SPINNER":
				var spinnerData spinnerJSON
				dec := json.NewDecoder(jsonStream)
				if err := dec.Decode(&spinnerData); err != nil {
					fmt.Println("Can not decode spinner json", err)
					continue
				}
				pb.Spin(spinnerData.Message)
			case key == "ERROR":
				var errorData string
				dec := json.NewDecoder(jsonStream)
				if err := dec.Decode(&errorData); err != nil {
					fmt.Println("Can not decode json: ", err)
					continue
				}
				err = fmt.Errorf("Error from %s: %s", systemImageCli, errorData)
			}
		}
		if err != nil {
			return err
		}

		if err := scanner.Err(); err != nil {
			return err
		}
	}

	return cmd.Wait()
}
