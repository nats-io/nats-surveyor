// Copyright 2025 The NATS Authors
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

// JszFilter represents the metric types that can be filtered
type JszFilter int

// Enum values for JszFilter
const (
	StreamTotalMessages JszFilter = iota
	StreamTotalBytes
	StreamFirstSeq
	StreamLastSeq
	StreamConsumerCount
	StreamSubjectCount
	ConsumerDeliveredConsumerSeq
	ConsumerDeliveredStreamSeq
	ConsumerAckFloorConsumerSeq
	ConsumerAckFloorStreamSeq
	ConsumerNumAckPending
	ConsumerNumPending
	ConsumerNumRedelivered
	ConsumerNumWaiting
)

// JszFilterIds maps enum values to their string representations
var JszFilterIds = map[JszFilter][]string{
	StreamTotalMessages:          {"stream_total_messages"},
	StreamTotalBytes:             {"stream_total_bytes"},
	StreamFirstSeq:               {"stream_first_seq"},
	StreamLastSeq:                {"stream_last_seq"},
	StreamConsumerCount:          {"stream_consumer_count"},
	StreamSubjectCount:           {"stream_subject_count"},
	ConsumerDeliveredConsumerSeq: {"consumer_delivered_consumer_seq"},
	ConsumerDeliveredStreamSeq:   {"consumer_delivered_stream_seq"},
	ConsumerAckFloorConsumerSeq:  {"consumer_ack_floor_consumer_seq"},
	ConsumerAckFloorStreamSeq:    {"consumer_ack_floor_stream_seq"},
	ConsumerNumAckPending:        {"consumer_num_ack_pending"},
	ConsumerNumPending:           {"consumer_num_pending"},
	ConsumerNumRedelivered:       {"consumer_num_redelivered"},
	ConsumerNumWaiting:           {"consumer_num_waiting"},
}

// JszFiltersToStringSlice converts a slice of JszFilter enums to their string representations
func JszFiltersToStringSlice(filters []JszFilter) []string {
	result := make([]string, 0, len(filters))
	for _, filter := range filters {
		if ids, ok := JszFilterIds[filter]; ok && len(ids) > 0 {
			result = append(result, ids[0])
		}
	}
	return result
}
