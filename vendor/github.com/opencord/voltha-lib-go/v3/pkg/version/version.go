/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package version

import (
	"fmt"
	"strings"
)

// Default build-time variable.
// These values can (should) be overridden via ldflags when built with
// `make`
var (
	version   = "unknown-version"
	goVersion = "unknown-goversion"
	vcsRef    = "unknown-vcsref"
	vcsDirty  = "unknown-vcsdirty"
	buildTime = "unknown-buildtime"
	os        = "unknown-os"
	arch      = "unknown-arch"
)

type VersionInfoType struct {
	Version   string `json:"version"`
	GoVersion string `json:"goversion"`
	VcsRef    string `json:"vcsref"`
	VcsDirty  string `json:"vcsdirty"`
	BuildTime string `json:"buildtime"`
	Os        string `json:"os"`
	Arch      string `json:"arch"`
}

var VersionInfo VersionInfoType

func init() {
	VersionInfo = VersionInfoType{
		Version:   version,
		VcsRef:    vcsRef,
		VcsDirty:  vcsDirty,
		GoVersion: goVersion,
		Os:        os,
		Arch:      arch,
		BuildTime: buildTime,
	}
}

func (v VersionInfoType) String(indent string) string {
	builder := strings.Builder{}

	builder.WriteString(fmt.Sprintf("%sVersion:      %s\n", indent, VersionInfo.Version))
	builder.WriteString(fmt.Sprintf("%sGoVersion:    %s\n", indent, VersionInfo.GoVersion))
	builder.WriteString(fmt.Sprintf("%sVCS Ref:      %s\n", indent, VersionInfo.VcsRef))
	builder.WriteString(fmt.Sprintf("%sVCS Dirty:    %s\n", indent, VersionInfo.VcsDirty))
	builder.WriteString(fmt.Sprintf("%sBuilt:        %s\n", indent, VersionInfo.BuildTime))
	builder.WriteString(fmt.Sprintf("%sOS/Arch:      %s/%s\n", indent, VersionInfo.Os, VersionInfo.Arch))
	return builder.String()
}
