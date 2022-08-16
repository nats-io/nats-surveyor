// Copyright 2022 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cmd

import (
	"strings"
	"testing"
)

func TestRootCmdArgs(t *testing.T) {
	rootCmdArgsTests := []struct {
		name string
		in   []string
		out  string
	}{
		{
			"single",
			[]string{"-version"},
			"--version",
		},
		{
			"compound",
			[]string{"-http_tlscert", "a", "--http_tlskey", "b", "-http_tlscacert=c", "-jetstream=/jetstream", "-observe=/observe", "--", "-version"},
			"--http-tlscert a --http-tlskey b --http-tlscacert=c --jetstream=/jetstream --observe=/observe -- -version",
		},
	}

	for _, tt := range rootCmdArgsTests {
		t.Run(tt.name, func(t *testing.T) {
			result := strings.Join(rootCmdArgs(tt.in), " ")
			if result != tt.out {
				t.Errorf("rootCmdArgs(%s) got '%s', want '%s'", tt.name, result, tt.out)
			}
		})
	}
}
