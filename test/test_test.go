// Copyright 2019 The NATS Authors
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

// Package test has test functions for the surveyor
package test

import (
	"testing"
)

// Keep our coverage up

func TestStartBasicServer(t *testing.T) {
	s := StartBasicServer()
	s.Shutdown()
}

func TestStartSupercluster(t *testing.T) {
	ns := NewSuperCluster(t)
	ns.Shutdown()
}
