// Copyright 2017 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package register

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/coreos/init/tests/util"
)

type Test struct {
	Name string
	Func func(*testing.T, Test)
}

func (test Test) Run(t *testing.T) {
	if os.Getenv("TMPDIR") == "" {
		tmpDir, err := ioutil.TempDir("/var/tmp", "")
		if err != nil {
			t.Fatalf("failed to create temp working dir in /var/tmp: %v", err)
		}

		err = os.Setenv("TMPDIR", tmpDir)
		if err != nil {
			t.Fatalf("couldn't set TMPDIR env var: %v", err)
		}

		defer test.RemoveAll(t, tmpDir)
		defer os.Setenv("TMPDIR", "")
	}
	test.Func(t, test)
}

func (test Test) CreateDevice(t *testing.T) (string, string) {
	diskFile, err := ioutil.TempFile("", "coreos-install-disk")
	if err != nil {
		t.Fatalf("failed to create disk file: %v", err)
	}

	// truncate the disk file to 10GB, this should be large enough
	err = os.Truncate(diskFile.Name(), 10*1024*1024*1024)
	if err != nil {
		t.Fatalf("failed to truncate disk file: %v", err)
	}

	// create a gpt table
	util.MustRun(t, "sgdisk", diskFile.Name())

	// back a loop device with the disk file
	device := string(util.MustRun(t, "losetup", "-P", "-f", diskFile.Name(), "--show"))
	return diskFile.Name(), strings.TrimSpace(device)
}

func (test Test) CleanupDisk(t *testing.T, diskFile, loopDevice string) {
	util.MustRun(t, "losetup", "-d", loopDevice)
	test.RemoveAll(t, diskFile)
}

func (test Test) CreateDeviceMappers(t *testing.T, diskFile string) (devices []string) {
	out := util.MustRun(t, "kpartx", "-avs", diskFile)
	devices = util.RegexpSearchAll(t, "loop device", "map (?P<device>[\\w\\d]+)", out)

	t.Logf("kpartx out: %s", string(out))

	for i, d := range devices {
		devices[i] = fmt.Sprintf("/dev/mapper/%s", d)
	}
	return
}

func (test Test) RemoveDeviceMappers(t *testing.T, diskFile string) {
	util.MustRun(t, "kpartx", "-d", diskFile)
}

func (test Test) MountDeviceMapper(t *testing.T, device string) string {
	dir, err := ioutil.TempDir("", "coreos-install-mount-point")
	if err != nil {
		t.Fatalf("couldn't create mount point directory: %v", err)
	}

	err = util.Run(t, "mount", device, dir, "-o", "ro")
	if err != nil {
		return ""
	}

	return dir
}

func (test Test) UnmountPath(t *testing.T, path string) {
	util.MustRun(t, "umount", path)
}

func WhichCoreosInstall(t *testing.T) string {
	out, err := exec.Command("which", "coreos-install").CombinedOutput()
	if err != nil {
		return ""
	}

	return filepath.Dir(string(out))
}

func (test Test) RunCoreOSInstall(t *testing.T, opts ...string) {
	util.MustRun(t, "coreos-install", opts...)
}

func (test Test) ValidateIgnition(t *testing.T, mountPaths []string, config string) {
	ignition_found := false
	grub_found := false
	for _, p := range mountPaths {
		ignition_path := filepath.Join(p, "coreos-install.json")
		if _, err := os.Stat(ignition_path); !os.IsNotExist(err) {
			ignition_found = true
			data, err := ioutil.ReadFile(ignition_path)
			if err != nil {
				t.Fatalf("couldn't read coreos-install.json: %v", err)
			}

			if string(data) != config {
				t.Fatalf("coreos-install.json doesn't match: expected %s, received %s", config, data)
			}

		}

		grub_path := filepath.Join(p, "grub.cfg")
		if _, err := os.Stat(grub_path); !os.IsNotExist(err) {
			grub_found = true
			data, err := ioutil.ReadFile(grub_path)
			if err != nil {
				t.Fatalf("couldn't read grub.cfg: %v", err)
			}

			util.RegexpContains(t, "ignition grub.cfg info", "coreos.config.url=oem:///coreos-install.json", data)
		}
	}

	if !ignition_found {
		t.Fatalf("couldn't find coreos-install.json")
	}

	if !grub_found {
		t.Fatalf("couldn't find grub.cfg")
	}
}

func (test Test) RemoveAll(t *testing.T, path string) {
	err := os.RemoveAll(path)
	if err != nil {
		t.Errorf("couldn't remove %s: %v", path, err)
	}
}

func (test Test) WriteFile(t *testing.T, data string) string {
	tmpFile, err := ioutil.TempFile("", "coreos-install-file")
	if err != nil {
		t.Fatalf("failed creating tmp file: %v", err)
	}

	writer := bufio.NewWriter(tmpFile)
	_, err = writer.WriteString(data)
	if err != nil {
		t.Fatalf("writing to tmp file failed: %v", err)
	}
	writer.Flush()

	return tmpFile.Name()
}

func (test Test) ValidateCloudinit(t *testing.T, mountPaths []string, config string) {
	cloudinit_found := false

	for _, p := range mountPaths {
		path := filepath.Join(p, "var", "lib", "coreos-install", "user_data")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			cloudinit_found = true
			data, err := ioutil.ReadFile(path)
			if err != nil {
				t.Fatalf("couldn't read coreos-install/user_data: %v", err)
			}

			if string(data) != config {
				t.Fatalf("coreos-install/user_data doesn't match: expected %s, received %s", config, data)
			}
		}
	}

	if !cloudinit_found {
		t.Fatalf("couldn't find coreos-install/user_data")
	}

}

// searches for /usr/lib/os-release on all mount paths given
func (test Test) ReleaseExists(t *testing.T, mountPaths []string) {
	releaseExists := false
	for _, p := range mountPaths {
		releasePath := filepath.Join(p, "lib", "os-release")
		if _, err := os.Stat(releasePath); !os.IsNotExist(err) {
			releaseExists = true
			break
		}
	}
	if !releaseExists {
		t.Fatalf("/usr/lib/os-release not found on any partitions")
	}
}

func (test Test) ValidatePartitionLabel(t *testing.T, diskFile, expectedLabel string, rootPartNum int) {
	diskInfo := util.MustRun(t, "sgdisk", "-i", strconv.Itoa(rootPartNum), diskFile)

	actualLabel := util.RegexpSearch(t, "partition name", "Partition name: '(?P<name>[\\d\\w-_]+)'", diskInfo)

	if expectedLabel != actualLabel {
		t.Fatalf("label on partition %d did not match. expected %s, received %s", rootPartNum, expectedLabel, actualLabel)
	}
}

func (test Test) ValidateDefaultRootPartition(t *testing.T, diskFile string) {
	test.ValidatePartitionLabel(t, diskFile, "ROOT", 9)
}

func (test Test) ValidateDefaultUSRAPartition(t *testing.T, diskFile string) {
	test.ValidatePartitionLabel(t, diskFile, "USR-A", 3)
}

func (test Test) DefaultChecks(t *testing.T, mountPaths []string, diskFile string) {
	test.ReleaseExists(t, mountPaths)
	test.ValidateDefaultRootPartition(t, diskFile)
	test.ValidateDefaultUSRAPartition(t, diskFile)
}

var Tests []Test

func Register(t Test) {
	Tests = append(Tests, t)
}
