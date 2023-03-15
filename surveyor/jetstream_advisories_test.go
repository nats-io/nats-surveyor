package surveyor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/jsm.go"
	st "github.com/nats-io/nats-surveyor/test"
	"github.com/prometheus/client_golang/prometheus"
	ptu "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJetStream_Load(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opt := GetDefaultOptions()
	opt.URLs = js.ClientURL()
	metrics := NewJetStreamAdvisoryMetrics(prometheus.NewRegistry(), nil)
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})
	cp := newSurveyorConnPool(opt, reconnectCtr)

	config, err := NewJetStreamAdvisoryConfigFromFile("testdata/goodjs/global.json")
	if err != nil {
		t.Fatalf("advisory config error: %s", err)
	}
	adv, err := newJetStreamAdvisoryListener(config, cp, opt.Logger, metrics)
	if err != nil {
		t.Fatalf("advisory listener error: %s", err)
	}
	err = adv.Start()
	if err != nil {
		t.Fatalf("advisory start error: %s", err)
	}
	adv.Stop()

	_, err = NewJetStreamAdvisoryConfigFromFile("testdata/badjs/missing.json")
	if err.Error() != "open testdata/badjs/missing.json: no such file or directory" {
		t.Fatalf("jetstream load error: %s", err)
	}

	_, err = NewJetStreamAdvisoryConfigFromFile("testdata/badjs/bad.json")
	if err.Error() != "invalid JetStream advisory config: testdata/badjs/bad.json: name is required" {
		t.Fatalf("jetstream load error: %s", err)
	}

	config, err = NewJetStreamAdvisoryConfigFromFile("testdata/badjs/badauth.json")
	if err != nil {
		t.Fatalf("observation config error: %s", err)
	}
	adv, err = newJetStreamAdvisoryListener(config, cp, opt.Logger, metrics)
	if err != nil {
		t.Fatalf("observation listener error: %s", err)
	}
	err = adv.Start()
	if err.Error() != "nats connection failed for id: testdata/badjs/badauth.json, account name: testing, error: nats: Authorization Violation" {
		t.Fatalf("observation load error does not match expected error: %s", err)
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
	metrics := NewJetStreamAdvisoryMetrics(prometheus.NewRegistry(), nil)
	reconnectCtr := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName("nats", "survey", "nats_reconnects"),
		Help: "Number of times the surveyor reconnected to the NATS cluster",
	}, []string{"name"})
	cp := newSurveyorConnPool(opt, reconnectCtr)

	config, err := NewJetStreamAdvisoryConfigFromFile("testdata/goodjs/global.json")
	if err != nil {
		t.Fatalf("advisory config error: %s", err)
	}
	adv, err := newJetStreamAdvisoryListener(config, cp, opt.Logger, metrics)
	if err != nil {
		t.Fatalf("advisory listener error: %s", err)
	}
	err = adv.Start()
	if err != nil {
		t.Fatalf("advisory start error: %s", err)
	}
	defer adv.Stop()

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

func TestSurveyor_AdvisoriesFromFile(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opts := getTestOptions()
	opts.URLs = js.ClientURL()
	opts.JetStreamConfigDir = "testdata/goodjs"

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()

	if ptu.ToFloat64(s.JetStreamAdvisoryManager().metrics.jsAdvisoriesGauge) != 1 {
		t.Fatalf("process error: advisories not started")
	}
}

func TestSurveyor_Advisories(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opts := getTestOptions()
	opts.URLs = js.ClientURL()

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()
	am := s.JetStreamAdvisoryManager()

	expectedAdvisories := make(map[string]*JSAdvisoryConfig)
	advisories := []*JSAdvisoryConfig{
		{
			ID:          "a",
			AccountName: "a",
			Username:    "a",
			Password:    "a",
		},
		{
			ID:          "b",
			AccountName: "b",
			Username:    "b",
			Password:    "b",
		},
		{
			ID:          "c",
			AccountName: "c",
			Nkey:        "../test/c.nkey",
		},
	}

	advIDs := make([]string, 0)
	for _, adv := range advisories {
		err := am.Set(adv)
		if err != nil {
			t.Errorf("Unexpected error on advisory set: %s", err)
		}
		advIDs = append(advIDs, adv.ID)
		expectedAdvisories[adv.ID] = adv
	}
	waitForAdvUpdate(t, am, expectedAdvisories)

	setAdvisory := &JSAdvisoryConfig{
		ID:          advIDs[0],
		AccountName: "aa",
		Username:    "a",
		Password:    "a",
	}
	expectedAdvisories[advIDs[0]] = setAdvisory
	err = am.Set(setAdvisory)
	if err != nil {
		t.Errorf("Unexpected error on advisory set: %s", err)
	}
	waitForAdvUpdate(t, am, expectedAdvisories)
	var found bool
	advMap := am.ConfigMap()
	for _, adv := range advMap {
		if adv.AccountName == "aa" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected updated account name in advisory: %s", "aa")
	}
	deleteID := advIDs[0]
	err = am.Delete(deleteID)
	delete(expectedAdvisories, deleteID)
	if err != nil {
		t.Errorf("Unexpected error on advisory delete request: %s", err)
	}
	waitForAdvUpdate(t, am, expectedAdvisories)

	// advisory no longer exists
	err = am.Delete(deleteID)
	if err == nil {
		t.Error("Expected error; got nil")
	}
	waitForAdvUpdate(t, am, expectedAdvisories)
}

func TestSurveyor_AdvisoriesError(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opts := getTestOptions()
	opts.URLs = js.ClientURL()

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()
	om := s.JetStreamAdvisoryManager()
	if err != nil {
		t.Fatalf("Error creating advisories manager: %s", err)
	}

	// add invalid advisory (missing account name)
	err = om.Set(
		&JSAdvisoryConfig{
			ID:          "id",
			AccountName: "",
		},
	)

	if err == nil {
		t.Errorf("Expected error; got nil")
	}

	// valid advisory, no error
	err = om.Set(
		&JSAdvisoryConfig{
			ID:          "id",
			AccountName: "global",
		},
	)

	if err != nil {
		t.Errorf("Expected no error; got: %s", err)
	}

	// update error, invalid config
	err = om.Set(
		&JSAdvisoryConfig{
			ID:          "id",
			AccountName: "",
		},
	)

	if err == nil {
		t.Errorf("Expected error; got nil")
	}
}

func waitForAdvUpdate(t *testing.T, am *JSAdvisoryManager, expectedAdvisories map[string]*JSAdvisoryConfig) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	timeout := time.After(5 * time.Second)
	defer ticker.Stop()
Outer:
	for {
		select {
		case <-ticker.C:
			advisoriesNum := ptu.ToFloat64(am.metrics.jsAdvisoriesGauge)
			if advisoriesNum == float64(len(expectedAdvisories)) {
				break Outer
			}
		case <-timeout:
			advisoriesNum := ptu.ToFloat64(am.metrics.jsAdvisoriesGauge)
			t.Fatalf("process error: invalid number of advisories; want: %d; got: %f\n", len(expectedAdvisories), advisoriesNum)
			return
		}
	}

	existingAdvisories := am.ConfigMap()
	if len(existingAdvisories) != len(expectedAdvisories) {
		t.Fatalf("Unexpected number of advisories; want: %d; got: %d", len(expectedAdvisories), len(existingAdvisories))
	}
	for _, existingAdvisory := range existingAdvisories {
		obs, ok := expectedAdvisories[existingAdvisory.ID]
		if !ok {
			t.Fatalf("Missing advisory with ID: %s", existingAdvisory.ID)
		}
		if !reflect.DeepEqual(obs, existingAdvisory) {
			t.Fatalf("Invalid advisory config; want: %+v; got: %+v", obs, existingAdvisory)
		}
	}
}

func TestSurveyor_AdvisoriesWatcher(t *testing.T) {
	js := st.NewJetStreamServer(t)
	defer js.Shutdown()

	opts := getTestOptions()
	opts.URLs = js.ClientURL()

	dirName := fmt.Sprintf("testdata/adv%d", time.Now().UnixNano())
	if err := os.Mkdir(dirName, 0o700); err != nil {
		t.Fatalf("Error creating advisories dir: %s", err)
	}
	defer os.RemoveAll(dirName)
	opts.JetStreamConfigDir = dirName

	s, err := NewSurveyor(opts)
	if err != nil {
		t.Fatalf("couldn't create surveyor: %v", err)
	}
	if err = s.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	defer s.Stop()
	time.Sleep(200 * time.Millisecond)

	am := s.JetStreamAdvisoryManager()
	expectedAdvisories := make(map[string]*JSAdvisoryConfig)

	t.Run("write advisory file - create operation", func(t *testing.T) {
		advConfig := &JSAdvisoryConfig{
			AccountName: "a",
			Username:    "a",
			Password:    "a",
		}
		advConfigJSON, err := json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		advPath := fmt.Sprintf("%s/create.json", dirName)
		if err := os.WriteFile(advPath, advConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing advisory config file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)
	})

	t.Run("first create then write to file - write operation", func(t *testing.T) {
		advConfig := &JSAdvisoryConfig{
			AccountName: "b",
			Username:    "b",
			Password:    "b",
		}
		advConfigJSON, err := json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		advPath := fmt.Sprintf("%s/write.json", dirName)
		f, err := os.Create(advPath)
		if err != nil {
			t.Fatalf("Error writing advisory config file: %s", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("Error closing file: %s", err)
		}
		time.Sleep(200 * time.Millisecond)
		if err := os.WriteFile(advPath, advConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)
	})

	t.Run("create advisories in subfolder", func(t *testing.T) {
		advConfig := &JSAdvisoryConfig{
			AccountName: "c",
			Credentials: "../test/c.nkey",
		}
		advConfigJSON, err := json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}

		if err := os.Mkdir(fmt.Sprintf("%s/subdir", dirName), 0o700); err != nil {
			t.Fatalf("Error creating subdirectory: %s", err)
		}
		time.Sleep(100 * time.Millisecond)

		advPath := fmt.Sprintf("%s/subdir/subadv.json", dirName)

		err = os.WriteFile(advPath, advConfigJSON, 0o600)
		if err != nil {
			t.Fatalf("Error writing advisory config file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)

		advConfig = &JSAdvisoryConfig{
			AccountName: "d",
			Username:    "d",
			Password:    "d",
		}
		advConfigJSON, err = json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		advPath = fmt.Sprintf("%s/subdir/abc.json", dirName)

		if err := os.WriteFile(advPath, advConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing advisory config file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)

		advConfig = &JSAdvisoryConfig{
			AccountName: "global",
		}
		advConfigJSON, err = json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}
		if err := os.Mkdir(fmt.Sprintf("%s/subdir/nested", dirName), 0o700); err != nil {
			t.Fatalf("Error creating subdirectory: %s", err)
		}
		time.Sleep(100 * time.Millisecond)

		advPath = fmt.Sprintf("%s/subdir/nested/nested.json", dirName)
		err = os.WriteFile(advPath, advConfigJSON, 0o600)
		if err != nil {
			t.Fatalf("Error writing advisory config file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)
	})

	t.Run("update advisories", func(t *testing.T) {
		advConfig := &JSAdvisoryConfig{
			AccountName: "bb",
			Username:    "b",
			Password:    "b",
		}
		advConfigJSON, err := json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}

		advPath := fmt.Sprintf("%s/write.json", dirName)
		if err := os.WriteFile(advPath, advConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)

		// update file with invalid JSON - existing advisory should not be impacted
		if err := os.WriteFile(advPath, []byte("abc"), 0o600); err != nil {
			t.Fatalf("Error writing to file: %s", err)
		}
		time.Sleep(100 * time.Millisecond)
		waitForAdvUpdate(t, am, expectedAdvisories)
	})

	t.Run("remove advisories", func(t *testing.T) {
		// remove single advisory
		advPath := fmt.Sprintf("%s/create.json", dirName)
		if err := os.Remove(advPath); err != nil {
			t.Fatalf("Error removing advisory config: %s", err)
		}
		delete(expectedAdvisories, advPath)
		waitForAdvUpdate(t, am, expectedAdvisories)

		// remove whole subfolder
		if err := os.RemoveAll(fmt.Sprintf("%s/subdir", dirName)); err != nil {
			t.Fatalf("Error removing subdirectory: %s", err)
		}

		delete(expectedAdvisories, fmt.Sprintf("%s/subdir/subadv.json", dirName))
		delete(expectedAdvisories, fmt.Sprintf("%s/subdir/abc.json", dirName))
		delete(expectedAdvisories, fmt.Sprintf("%s/subdir/nested/nested.json", dirName))
		waitForAdvUpdate(t, am, expectedAdvisories)

		advConfig := &JSAdvisoryConfig{
			AccountName: "aa",
			Username:    "a",
			Password:    "a",
		}
		advConfigJSON, err := json.Marshal(advConfig)
		if err != nil {
			t.Fatalf("marshalling error: %s", err)
		}

		advPath = fmt.Sprintf("%s/another.json", dirName)
		if err := os.WriteFile(advPath, advConfigJSON, 0o600); err != nil {
			t.Fatalf("Error writing advisory config file: %s", err)
		}

		advConfig.ID = advPath
		expectedAdvisories[advPath] = advConfig
		waitForAdvUpdate(t, am, expectedAdvisories)
	})
}
