package recovery

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/logger"
)

func GetKernelParameter(name string) string {
	f, err := os.Open("/proc/cmdline")
	if err != nil {
		return ""
	}
	defer f.Close()
	cmdline, err := ioutil.ReadAll(f)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(fmt.Sprintf(`\b%s=([A-Za-z0-9_-]*)\b`, name))
	match := re.FindSubmatch(cmdline)
	if len(match) < 2 {
		return ""
	}
	return string(match[1])
}

func globFile(dir, pattern string) string {
	files, err := filepath.Glob(path.Join(dir, pattern))
	if err != nil {
		return ""
	}
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

func mount(label, mountpoint string) error {
	dev, err := getDevByLabel(label)
	if err != nil {
		return err
	}
	logger.Noticef("mount %s on %s", dev, mountpoint)
	if err := exec.Command("mount", dev, mountpoint).Run(); err != nil {
		return fmt.Errorf("cannot mount device %s: %s", dev, err)
	}
	logger.Noticef("%s mounted", dev)

	return nil
}

func umount(dev string) error {
	logger.Noticef("unmount %s", dev)
	if err := exec.Command("umount", dev).Run(); err != nil {
		return fmt.Errorf("cannot unmount device %s: %s", dev, err)
	}

	return nil
}

func mkdirs(base string, dirlist []string, mode os.FileMode) error {
	for _, dir := range dirlist {
		p := path.Join(base, dir)
		logger.Noticef("mkdir %s", p)
		if err := os.MkdirAll(p, mode); err != nil {
			return fmt.Errorf("cannot create directory %s/%s: %s", base, dir, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	// FIXME
	logger.Noticef("copying %s", src)
	if err := exec.Command("cp", "-rap", src, dst).Run(); err != nil {
		return fmt.Errorf("cannot copy tree from %s to %s: %s", src, dst, err)
	}
	return nil
}
