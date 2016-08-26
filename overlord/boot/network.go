package boot

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

var netplanConfigFile = "/run/netplan/00-initial-config.yaml"
var enableConfig = []string{"netplan", "apply"}

var netplanConfigData = `
network:
 version: 2
 ethernets:
   all:
    match:
     name: "*"
    dhcp4: true
`

var osRemove = os.Remove

// InitialNetworkConfig writes and applies a netplan config that
// enables dhcp on all wired interfaces. In the long run this should
// be run as part of the config-changed hook and read the snap's
// config to determine the netplan config to write.
func InitialNetworkConfig() error {
	if err := os.MkdirAll(filepath.Dir(netplanConfigFile), 0755); err != nil {
		return err
	}
	if err := osutil.AtomicWriteFile(netplanConfigFile, []byte(netplanConfigData), 0644, 0); err != nil {
		return err
	}

	enable := exec.Command(enableConfig[0], enableConfig[1:]...)
	enable.Stdout = os.Stdout
	enable.Stderr = os.Stderr
	if err := enable.Run(); err != nil {
		return err
	}
	if err := osRemove(netplanConfigFile); err != nil {
		return err
	}

	return nil
}
