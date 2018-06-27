package crawler

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pilebones/go-udev/netlink"
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
		if err := matcher.Compile(); err != nil {
			errors <- fmt.Errorf("Wrong matcher, err: %v", err)
			quit <- struct{}{}
			return quit
		}
	}

	go func() {
		err := filepath.Walk(BASE_DEVPATH, func(path string, info os.FileInfo, err error) error {
			select {
			case <-quit:
				return fmt.Errorf("abort signal receive")
			default:
				if err != nil {
					return err
				}

				if info.IsDir() || info.Name() != "uevent" {
					return nil
				}

				env, err := getEventFromUEventFile(path)
				if err != nil {
					return err
				}

				kObj := filepath.Dir(path)

				// Append to env subsystem if existing
				if link, err := os.Readlink(kObj + "/subsystem"); err == nil {
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
		})

		if err != nil {
			errors <- err
		}
		close(queue)
	}()
	return quit
}

// getEventFromUEventFile return all env var define in file
// syntax: name=value for each line
// Fonction use for /sys/.../uevent files
func getEventFromUEventFile(path string) (rv map[string]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

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
