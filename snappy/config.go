package snappy

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mvo5/goconfigparser"
)

func snapConfig(snapDir, rawConfig string) error {
	configScript := filepath.Join(snapDir, "hooks", "config")
	cmd := exec.Command(configScript)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	// meh, really golang?
	go func() {
		defer stdin.Close()
		io.Copy(stdin, strings.NewReader(rawConfig))
	}()

	output, err := cmd.Output()
	if err != nil {
		return err
	}

	// check he output,
	// FIXME: goyaml does fail for me for the output "ok: true" :(
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadString(string(output)); err != nil {
		return err
	}
	v, err := cfg.Get("", "ok")
	// FIXME: more clever bool conversion
	if err != nil || v != "true" {
		errorStr, _ := cfg.Get("", "error")
		return fmt.Errorf("config: '%s' failed with: '%s'", configScript, errorStr)
	}

	return nil
}
