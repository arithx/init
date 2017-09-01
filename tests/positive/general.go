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

package positive

import (
	"testing"

	"github.com/coreos/init/tests/register"
)

func init() {
	register.Register(register.Test{
		Name: "Does this thing work?",
		Func: baseTest,
	})
}

func baseTest(t *testing.T, test register.Test) {
	diskFile, loopDevice := test.CreateDevice(t)
	defer test.CleanupDisk(t, diskFile, loopDevice)

	ignition_config := `{
		"ignition": {
			"version": "2.1.0"
		}
	}`
	ignition := test.WriteFile(t, ignition_config)
	defer test.RemoveAll(t, ignition)

	opts := []string{
		"-d", loopDevice,
		"-i", ignition}

	test.RunCoreOSInstall(t, opts...)

	devices := test.CreateDeviceMappers(t, loopDevice)
	defer test.RemoveDeviceMappers(t, loopDevice)

	var mountPaths []string
	for _, device := range devices {
		path := test.MountDeviceMapper(t, device)
		if path != "" {
			mountPaths = append(mountPaths, path)
			defer test.RemoveAll(t, path)
			defer test.UnmountPath(t, path)
		}
	}

	test.DefaultChecks(t, mountPaths, diskFile)
	test.ValidateIgnition(t, mountPaths, ignition_config)
}
