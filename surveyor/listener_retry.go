// Copyright 2026 The NATS Authors
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

package surveyor

import "time"

// Listeners (JetStream advisories, service observations) whose Start fails are
// kept by their manager and retried with exponential backoff instead of being
// dropped. These values are variables so tests can shorten them.
var (
	// listenerRetryTick is how often a manager checks for listeners due for a retry.
	listenerRetryTick = time.Second
	// listenerRetryMinBackoff is the delay before the first retry.
	listenerRetryMinBackoff = time.Second
	// listenerRetryMaxBackoff caps the delay between retries.
	listenerRetryMaxBackoff = time.Minute
)

// listenerRetryDelay returns the backoff delay after the given number of failed attempts (>= 1).
func listenerRetryDelay(attempts int) time.Duration {
	delay := listenerRetryMinBackoff
	for i := 1; i < attempts && delay < listenerRetryMaxBackoff; i++ {
		delay *= 2
	}
	if delay > listenerRetryMaxBackoff {
		delay = listenerRetryMaxBackoff
	}
	return delay
}
