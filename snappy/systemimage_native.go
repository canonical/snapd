package snappy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
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
