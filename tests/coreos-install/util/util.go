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

package util

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TryRegexpSearch(name, pattern string, data []byte) (string, error) {
	re := regexp.MustCompile(pattern)
	match := re.FindSubmatch(data)
	if len(match) < 2 {
		return "", fmt.Errorf("didn't find %s", name)
	}
	return string(match[1]), nil
}

func RegexpSearch(t *testing.T, itemName, pattern string, data []byte) string {
	re := regexp.MustCompile(pattern)
	match := re.FindSubmatch(data)
	if len(match) < 2 {
		t.Fatalf("couldn't find %s", itemName)
	}
	return string(match[1])
}

func RegexpContains(t *testing.T, pattern string, data []byte) bool {
	re := regexp.MustCompile(pattern)
	match := re.FindSubmatch(data)
	return len(match) > 0
}

func RegexpSearchAll(t *testing.T, itemName, pattern string, data []byte) (ret []string) {
	re := regexp.MustCompile(pattern)
	match := re.FindAllSubmatch(data, -1)
	if match == nil {
		t.Fatalf("couldn't find %s", itemName)
	}

	for _, m := range match {
		ret = append(ret, string(m[1]))
	}
	return
}

func MustRun(t *testing.T, command string, opts ...string) []byte {
	out, err := exec.Command(command, opts...).CombinedOutput()
	if err != nil {
		t.Log(string(out))
		t.Fatalf("%s %s failed: %v", command, strings.Join(opts, " "), err)
	}
	return out
}

func Run(t *testing.T, command string, opts ...string) error {
	_, err := exec.Command(command, opts...).CombinedOutput()
	return err
}

func StringToPtr(str string) *string {
	return &str
}

func FetchLocalImage(t *testing.T) string {
	tmpPath := os.Getenv("TMPDIR")
	if tmpPath == "" {
		tmpPath = "/var/tmp"
	}

	tmpDir, err := ioutil.TempDir(tmpPath, "")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}

	err = DownloadFile(t, tmpDir, "coreos_production_image.bin.bz2")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed downloading image: %v", err)
	}

	err = DownloadFile(t, tmpDir, "coreos_production_image.bin.bz2.sig")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed downloading signature: %v", err)
	}

	err = DownloadFile(t, tmpDir, "version.txt")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed downloading version: %v", err)
	}

	return tmpDir
}

// Used to get defaults for channel, board, & version, first checks if the
// host machine is Container Linux and if so uses the data from the machine
// otherwise defaults to stable, amd64-usr, & current respectively
func GetDefaultChannelBoardVersion(t *testing.T) (string, string, string) {
	data, err := ioutil.ReadFile("/usr/lib/os-release")
	if err != nil {
		return "stable", "amd64-usr", "current"
	}

	os, err := TryRegexpSearch("id", "ID=['\"]?([A-Za-z0-9 \\._\\-]*)['\"]?", data)
	if err != nil || os != "coreos" {
		return "stable", "amd64-usr", "current"
	}

	version := RegexpSearch(t, "version", "VERSION_ID=['\"]?([A-Za-z0-9 \\._\\-]*)['\"]?", data)
	board := RegexpSearch(t, "board", "COREOS_BOARD=['\"]?([A-Za-z0-9 \\._\\-]*)['\"]?", data)

	data, err = ioutil.ReadFile("/etc/coreos/update.conf")
	if err != nil {
		t.Fatalf("reading /etc/coreos/update.conf: %v", err)
	}

	channel := RegexpSearch(t, "channel", "GROUP=['\"]?([A-Za-z0-9 \\._\\-]*)['\"]?", data)

	return channel, board, version
}

func DownloadFile(t *testing.T, tmpDir, name string) error {
	file, err := os.Create(filepath.Join(tmpDir, name))
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	channel, board, version := GetDefaultChannelBoardVersion(t)

	resp, err := http.Get(fmt.Sprintf("https://%s.release.core-os.net/%s/%s/%s", channel, board, version, name))
	if err != nil {
		return fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed copying file data: %v", err)
	}

	return nil
}

type HTTPServer struct {
	FileDir string
}

func (server *HTTPServer) Version(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(server.FileDir, "version.txt"))
}

func (server *HTTPServer) Image(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(server.FileDir, "coreos_production_image.bin.bz2"))
}

func (server *HTTPServer) Signature(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(server.FileDir, "coreos_production_image.bin.bz2.sig"))
}

func (server *HTTPServer) Start(t *testing.T) string {
	http.HandleFunc("/current/version.txt", server.Version)

	data, err := ioutil.ReadFile(filepath.Join(server.FileDir, "version.txt"))
	if err != nil {
		t.Fatalf("Couldn't read version.txt")
	}
	version := RegexpSearch(t, "version", "COREOS_VERSION=(.*)", data)

	http.HandleFunc(fmt.Sprintf("/%s/coreos_production_image.bin.bz2", version), server.Image)
	http.HandleFunc(fmt.Sprintf("/%s/coreos_production_image.bin.bz2.sig", version), server.Signature)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("creating listener: %v", err)
	}

	go http.Serve(listener, nil)

	return listener.Addr().String()
}
