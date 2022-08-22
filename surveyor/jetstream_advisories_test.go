package surveyor

import (
	"bytes"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	st "github.com/nats-io/nats-surveyor/test"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJetStream_Load(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opt := GetDefaultOptions()
	opt.URLs = js.ClientURL()
	metrics := NewJetStreamAdvisoryMetrics(prometheus.NewRegistry())
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})

	obs, err := NewJetStreamAdvisoryListener("testdata/goodjs/global.json", *opt, metrics, reconnectCtr)
	if err != nil {
		t.Fatalf("jetstream load error: %s", err)
	}
	obs.Stop()

	_, err = NewJetStreamAdvisoryListener("testdata/badjs/missing.json", *opt, metrics, reconnectCtr)
	if err.Error() != "open testdata/badjs/missing.json: no such file or directory" {
		t.Fatalf("jetstream load error: %s", err)
	}

	_, err = NewJetStreamAdvisoryListener("testdata/badobs/bad.json", *opt, metrics, reconnectCtr)
	if err.Error() != "invalid JetStream advisory configuration: testdata/badobs/bad.json: name is required" {
		t.Fatalf("jetstream load error: %s", err)
	}
}

func TestJetStream_limitJSSubject(t *testing.T) {
	tests := [][]string{
		{"$JS.API.STREAM.CREATE.ORDERS", "$JS.API.STREAM.CREATE"},
		{"$JS.API.STREAM.MSG.GET.ORDERS", "$JS.API.STREAM.MSG.GET"},
		{"$JS.API.STREAM.LIST", "$JS.API.STREAM.LIST"},
		{"$JS.API.CONSUMER.CREATE.ORDERS", "$JS.API.CONSUMER.CREATE"},
		{"$JS.API.CONSUMER.DURABLE.CREATE.ORDERS.NEW", "$JS.API.CONSUMER.DURABLE.CREATE"},
	}

	for _, c := range tests {
		limited := limitJSSubject(c[0])
		if limited != c[1] {
			t.Fatalf("incorrect subject received: expected %q got %q", c[1], limited)
		}
	}
}

func TestJetStream_Handle(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opt := GetDefaultOptions()
	opt.URLs = js.ClientURL()
	metrics := NewJetStreamAdvisoryMetrics(prometheus.NewRegistry())
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})

	obs, err := NewJetStreamAdvisoryListener("testdata/goodjs/global.json", *opt, metrics, reconnectCtr)
	if err != nil {
		t.Fatalf("jetstream load error: %s", err)
	}
	defer obs.Stop()

	err = obs.Start()
	if err != nil {
		t.Fatalf("jetstream failed to start: %s", err)
	}

	nc, err := nats.Connect(js.ClientURL(), nats.UseOldRequestStyle())
	if err != nil {
		t.Fatalf("could not connect nats client: %s", err)
	}

	mgr, err := jsm.New(nc, jsm.WithTimeout(1100*time.Millisecond))
	if err != nil {
		t.Fatalf("could not get manager: %s", err)
	}

	if known, _ := mgr.IsKnownStream("SURVEYOR"); known {
		t.Fatalf("SURVEYOR stream already exist")
	}

	str, err := mgr.NewStream("SURVEYOR", jsm.Subjects("js.in.surveyor"), jsm.MemoryStorage())
	if err != nil {
		t.Fatalf("could not create stream: %s", err)
	}

	msg, err := nc.Request("js.in.surveyor", []byte("1"), time.Second)
	if err != nil {
		t.Fatalf("publish failed: %s", err)
	}
	if jsm.IsErrorResponse(msg) {
		t.Fatalf("publish failed: %s", string(msg.Data))
	}

	consumer, err := str.NewConsumer(jsm.AckWait(500*time.Millisecond), jsm.DurableName("OUT"), jsm.MaxDeliveryAttempts(1), jsm.SamplePercent(100))
	if err != nil {
		t.Fatalf("could not create consumer: %s", err)
	}

	consumer.NextMsg()
	consumer.NextMsg()

	msg, err = nc.Request("js.in.surveyor", []byte("2"), time.Second)
	if err != nil {
		t.Fatalf("publish failed: %s", err)
	}
	if jsm.IsErrorResponse(msg) {
		t.Fatalf("publish failed: %s", string(msg.Data))
	}

	msg, err = consumer.NextMsg()
	if err != nil {
		t.Fatalf("next failed: %s", err)
	}
	msg.Respond(nil)

	msg, err = nc.Request("js.in.surveyor", []byte("3"), time.Second)
	if err != nil {
		t.Fatalf("publish failed: %s", err)
	}
	if jsm.IsErrorResponse(msg) {
		t.Fatalf("publish failed: %s", string(msg.Data))
	}

	msg, err = consumer.NextMsg()
	if err != nil {
		t.Fatalf("next failed: %s", err)
	}
	msg.Nak()

	// time for advisories to be sent and handled
	time.Sleep(5 * time.Millisecond)

	expected := `
# HELP nats_jetstream_delivery_exceeded_count Advisories about JetStream Consumer Delivery Exceeded events
# TYPE nats_jetstream_delivery_exceeded_count counter
nats_jetstream_delivery_exceeded_count{account="global",consumer="OUT",stream="SURVEYOR"} 1
`
	err = ptu.CollectAndCompare(metrics.jsDeliveryExceededCtr, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("metrics failed: %s", err)
	}

	expected = `
# HELP nats_jetstream_api_audit JetStream API access audit events
# TYPE nats_jetstream_api_audit counter
nats_jetstream_api_audit{account="global",subject="$JS.API.CONSUMER.DURABLE.CREATE"} 1
nats_jetstream_api_audit{account="global",subject="$JS.API.STREAM.CREATE"} 1
nats_jetstream_api_audit{account="global",subject="$JS.API.STREAM.INFO"} 1
`
	err = ptu.CollectAndCompare(metrics.jsAPIAuditCtr, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("metrics failed: %s", err)
	}

	expected = `
# HELP nats_jetstream_acknowledgement_deliveries How many times messages took to be delivered and Acknowledged
# TYPE nats_jetstream_acknowledgement_deliveries counter
nats_jetstream_acknowledgement_deliveries{account="global",consumer="OUT",stream="SURVEYOR"} 1
`
	err = ptu.CollectAndCompare(metrics.jsAckMetricDeliveries, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("metrics failed: %s", err)
	}

	expected = `
	# HELP nats_jetstream_consumer_nak How many times a consumer sent a NAK
	# TYPE nats_jetstream_consumer_nak counter
	nats_jetstream_consumer_nak{account="global",consumer="OUT",stream="SURVEYOR"} 1
	`
	err = ptu.CollectAndCompare(metrics.jsConsumerDeliveryNAK, bytes.NewReader([]byte(expected)))
	if err != nil {
		t.Fatalf("metrics failed: %s", err)
	}
}
