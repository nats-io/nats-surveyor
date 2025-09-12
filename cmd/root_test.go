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

	"github.com/spf13/viper"
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
func TestTokenFileFlagExists(t *testing.T) {
	f := rootCmd.Flags().Lookup("token-file")
	if f == nil {
		t.Fatalf("expected --token-file flag to be registered on rootCmd")
	}
}

func TestTokenFileViper(t *testing.T) {
	if f := rootCmd.Flags().Lookup("token-file"); f != nil {
		_ = f.Value.Set("")
		f.Changed = false
	}
	if err := rootCmd.Flags().Set("token-file", "/tmp/aaa"); err != nil {
		t.Fatalf("failed setting flag: %v", err)
	}
	opts := getSurveyorOpts()
	if got, want := opts.TokenFile, "/tmp/aaa"; got != want {
		t.Fatalf("flag wiring failed: got %q, want %q", got, want)
	}
}

func TestTokenFileEnvOverride(t *testing.T) {
	if f := rootCmd.Flags().Lookup("token-file"); f != nil {
		_ = f.Value.Set("")
		f.Changed = false
	}

	viper.SetEnvPrefix("nats_surveyor")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	if err := viper.BindEnv("token-file"); err != nil {
		t.Fatalf("BindEnv failed: %v", err)
	}

	t.Setenv("NATS_SURVEYOR_TOKEN_FILE", "/tmp/bbb")

	opts := getSurveyorOpts()
	if got, want := opts.TokenFile, "/tmp/bbb"; got != want {
		t.Fatalf("env override failed: got %q, want %q", got, want)
	}
}
