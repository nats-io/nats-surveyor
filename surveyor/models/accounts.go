package models

import (
	"net/http"
	"time"
)

// ServiceLatency is the JSON message sent out in response to latency tracking for
// an accounts exported services. Additional client info is available in requestor
// and responder. Note that for a requestor, the only information shared by default
// is the RTT used to calculate the total latency. The requestor's account can
// designate to share the additional information in the service import.
type ServiceLatency struct {
	TypedEvent
	Status         int           `json:"status"`
	Error          string        `json:"description,omitempty"`
	Requestor      *ClientInfo   `json:"requestor,omitempty"`
	Responder      *ClientInfo   `json:"responder,omitempty"`
	RequestHeader  http.Header   `json:"header,omitempty"` // only contains header(s) triggering the measurement
	RequestStart   time.Time     `json:"start"`
	ServiceLatency time.Duration `json:"service"`
	SystemLatency  time.Duration `json:"system"`
	TotalLatency   time.Duration `json:"total"`
}
