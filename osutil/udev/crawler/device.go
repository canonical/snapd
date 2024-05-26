package crawler

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/udev/netlink"
)

const (
	BASE_DEVPATH = "/sys/devices"
)

type Device struct {
	KObj string
	Env  map[string]string
}

// ExistingDevices return all plugged devices matched by the matcher
// All uevent files inside /sys/devices is crawled to match right env values
func ExistingDevices(queue chan Device, errors chan error, matcher netlink.Matcher) chan struct{} {
	quit := make(chan struct{}, 1)

	if matcher != nil {
		mylog.Check(matcher.Compile())
	}

	go func() {
		mylog.Check(filepath.Walk(BASE_DEVPATH, func(path string, info os.FileInfo, err error) error {
			select {
			case <-quit:
				return fmt.Errorf("abort signal receive")
			default:

				if info.IsDir() || info.Name() != "uevent" {
					return nil
				}

				env := mylog.Check2(getEventFromUEventFile(path))

				kObj := filepath.Dir(path)

				// Append to env subsystem if existing
				if link := mylog.Check2(os.Readlink(kObj + "/subsystem")); err == nil {
					env["SUBSYSTEM"] = filepath.Base(link)
				}

				if matcher == nil || matcher.EvaluateEnv(env) {
					queue <- Device{
						KObj: kObj,
						Env:  env,
					}
				}
				return nil
			}
		}))

		close(queue)
	}()
	return quit
}

// getEventFromUEventFile return all env var define in file
// syntax: name=value for each line
// Fonction use for /sys/.../uevent files
func getEventFromUEventFile(path string) (rv map[string]string, err error) {
	f := mylog.Check2(os.Open(path))

	defer f.Close()

	data := mylog.Check2(io.ReadAll(f))

	rv = make(map[string]string, 0)
	buf := bufio.NewScanner(bytes.NewBuffer(data))

	var line string
	for buf.Scan() {
		line = buf.Text()
		field := strings.SplitN(line, "=", 2)
		if len(field) != 2 {
			return
		}
		rv[field[0]] = field[1]
	}

	return
}
