package snappy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func snapConfig(snapDir, rawConfig string) (newConfig string, err error) {
	configScript := filepath.Join(snapDir, "hooks", "config")
	if _, err := os.Stat(configScript); err != nil {
		return "", fmt.Errorf("No config for '%s'", snapDir)
	}
	return runConfigScript(configScript, rawConfig)
}

func runConfigScript(configScript, rawConfig string) (newConfig string, err error) {
	cmd := exec.Command(configScript)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}

	// meh, really golang?
	go func() {
		defer stdin.Close()
		io.Copy(stdin, strings.NewReader(rawConfig))
	}()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("config failed with: '%s'", output)
	}

	return string(output), nil
}
