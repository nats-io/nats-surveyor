package surveyor

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-server/v2/server"
	natsservertest "github.com/nats-io/nats-server/v2/test"
	"github.com/sirupsen/logrus"
	testifyAssert "github.com/stretchr/testify/assert"
)

func TestConnPool(t *testing.T) {
	t.Parallel()

	s := natsservertest.RunRandClientPortServer()
	defer s.Shutdown()
	o1 := &NatsContext{
		Name: "Client 1",
	}
	o2 := &NatsContext{
		Name: "Client 1",
	}
	o3 := &NatsContext{
		Name: "Client 2",
	}

	natsDefaults := &natsContextDefaults{
		URL: s.ClientURL(),
	}
	natsOptions := []nats.Option{
		nats.MaxReconnects(10240),
	}
	cp := newNatsConnPool(logrus.New(), natsDefaults, natsOptions)

	var c1, c2, c3 Conn
	var c1e, c2e, c3e error
	wg := &sync.WaitGroup{}
	wg.Add(3)
	go func() {
		c1, c1e = cp.Get(o1)
		wg.Done()
	}()
	go func() {
		c2, c2e = cp.Get(o2)
		wg.Done()
	}()
	go func() {
		c3, c3e = cp.Get(o3)
		wg.Done()
	}()
	wg.Wait()

	assert := testifyAssert.New(t)
	if assert.NoError(c1e) && assert.NoError(c2e) {
		assert.Same(c1, c2)
	}
	if assert.NoError(c3e) {
		assert.NotSame(c1, c3)
		assert.NotSame(c2, c3)
	}

	c1.Close()
	c3.Close()
	time.Sleep(1 * time.Second)
	assert.False(c1.Conn().IsClosed())
	assert.False(c2.Conn().IsClosed())
	assert.True(c3.Conn().IsClosed())

	c4, c4e := cp.Get(o1)
	if assert.NoError(c4e) {
		assert.Same(c2, c4)
	}

	c2.Close()
	c4.Close()
	time.Sleep(1 * time.Second)
	assert.True(c1.Conn().IsClosed())
	assert.True(c2.Conn().IsClosed())
	assert.True(c4.Conn().IsClosed())

	c5, c5e := cp.Get(o1)
	if assert.NoError(c5e) {
		assert.NotSame(c1, c5)
	}

	c5.Close()
	time.Sleep(1 * time.Second)
	assert.True(c5.Conn().IsClosed())
}

func writeTempFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	fp := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(fp, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return fp
}

func newPoolForTest(t *testing.T, url string) *natsConnPool {
	t.Helper()
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	def := &natsContextDefaults{Name: "test", URL: url}
	return newNatsConnPool(l, def, nil)
}

func runTokenServer(t *testing.T, token string) *server.Server {
	t.Helper()
	opts := &server.Options{
		Host:          "127.0.0.1",
		Port:          -1,
		Authorization: token,
	}
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	t.Cleanup(s.Shutdown)
	return s
}

func runUserPassServer(t *testing.T, user, pass string) *server.Server {
	t.Helper()
	return runUsersServer(t, &server.User{Username: user, Password: pass})
}

func runUsersServer(t *testing.T, users ...*server.User) *server.Server {
	t.Helper()
	opts := &server.Options{
		Host:  "127.0.0.1",
		Port:  -1,
		Users: users,
	}
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}
	t.Cleanup(s.Shutdown)
	return s
}

func authorizedUser(t *testing.T, s *server.Server, conn Conn) string {
	t.Helper()
	cid, err := conn.Conn().GetClientID()
	if err != nil {
		t.Fatalf("get client id: %v", err)
	}
	connz, err := s.Connz(&server.ConnzOptions{CID: cid, Username: true})
	if err != nil {
		t.Fatalf("connz: %v", err)
	}
	if len(connz.Conns) != 1 {
		t.Fatalf("expected 1 connection with cid %d, got %d", cid, len(connz.Conns))
	}
	return connz.Conns[0].AuthorizedUser
}

// Pooled connections for different credentials are created concurrently
// (e.g. by the service observation and JetStream advisory watchers). The
// options for one connection must not be visible to another, even when the
// pool's base options slice has spare capacity.
func TestConnPool_ConcurrentGet_UsesOwnCredentials(t *testing.T) {
	s := runUsersServer(t,
		&server.User{Username: "alice", Password: "alice-pass"},
		&server.User{Username: "bob", Password: "bob-pass"},
	)

	// Hold alice's connect inside option processing until bob's Get is done.
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseGate := func() { releaseOnce.Do(func() { close(release) }) }
	gate := func(*nats.Options) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil
	}

	baseOpts := make([]nats.Option, 0, 16)
	baseOpts = append(baseOpts, gate)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	cp := newNatsConnPool(logger, &natsContextDefaults{Name: "test", URL: s.ClientURL()}, baseOpts)

	var alice Conn
	var aliceErr error
	aliceDone := make(chan struct{})
	go func() {
		defer close(aliceDone)
		alice, aliceErr = cp.Get(&NatsContext{Username: "alice", Password: "alice-pass"})
	}()
	// release alice and close her connection on every exit path, failures included
	t.Cleanup(func() {
		releaseGate()
		<-aliceDone
		if alice != nil {
			alice.Close()
		}
	})

	select {
	case <-entered:
	case <-aliceDone:
		t.Fatalf("alice Get returned before connecting: %v", aliceErr)
	case <-time.After(5 * time.Second):
		t.Fatal("alice's connect did not start")
	}

	var bob Conn
	var bobErr error
	bobDone := make(chan struct{})
	go func() {
		defer close(bobDone)
		bob, bobErr = cp.Get(&NatsContext{
			Username: "bob",
			Password: "bob-pass",
			// a non-empty NatsOpts makes the first append in Get write into
			// the base slice's spare capacity, which also catches a fix that
			// clips only after that append
			NatsOpts: []nats.Option{nats.MaxReconnects(5)},
		})
	}()
	t.Cleanup(func() {
		releaseGate()
		<-bobDone
		if bob != nil {
			bob.Close()
		}
	})

	select {
	case <-bobDone:
	case <-time.After(5 * time.Second):
		t.Fatal("bob's Get blocked on alice's connect")
	}
	releaseGate()
	<-aliceDone

	if bobErr != nil {
		t.Fatalf("bob Get failed: %v", bobErr)
	}
	if aliceErr != nil {
		t.Fatalf("alice Get failed: %v", aliceErr)
	}

	for i, opt := range baseOpts[len(baseOpts):cap(baseOpts)] {
		if opt != nil {
			t.Errorf("Get wrote into the pool's base options at index %d", len(baseOpts)+i)
		}
	}
	if got := authorizedUser(t, s, alice); got != "alice" {
		t.Errorf("alice's pooled connection authenticated as %q", got)
	}
	if got := authorizedUser(t, s, bob); got != "bob" {
		t.Errorf("bob's pooled connection authenticated as %q", got)
	}
}

func TestConnPool_TokenFile_TokenAuth(t *testing.T) {
	const token = "abc123"
	s := runTokenServer(t, token)

	fp := writeTempFile(t, token)

	cp := newPoolForTest(t, s.ClientURL())
	conn, err := cp.Get(&NatsContext{
		TokenFile: fp,
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer conn.Close()

	if !conn.IsConnected() {
		t.Fatal("expected connected")
	}
}

func TestConnPool_Username_WithTokenFile_UserPassAuth(t *testing.T) {
	const user = "sys"
	const pass = "s3cr3t"

	s := runUserPassServer(t, user, pass)
	fp := writeTempFile(t, pass)

	cp := newPoolForTest(t, s.ClientURL())
	conn, err := cp.Get(&NatsContext{
		Username:  user,
		TokenFile: fp,
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer conn.Close()

	if !conn.IsConnected() {
		t.Fatal("expected connected")
	}
}

func TestConnPool_Username_WithTokenFile_AgainstTokenServer_Fails(t *testing.T) {
	const user = "sys"
	const pass = "s3cr3t"
	const token = "abc123"

	s := runTokenServer(t, token)
	fp := writeTempFile(t, pass)

	cp := newPoolForTest(t, s.ClientURL())
	_, err := cp.Get(&NatsContext{
		Username:  user,
		TokenFile: fp,
	})
	if err == nil {
		t.Fatalf("expected auth failure, but got success")
	}
}

func TestConnPool_TokenFile_TakesPrecedence_Over_StaticToken(t *testing.T) {
	const fileTok = "filetok"
	s := runTokenServer(t, fileTok)

	fp := writeTempFile(t, fileTok)

	cp := newPoolForTest(t, s.ClientURL())
	conn, err := cp.Get(&NatsContext{
		Token:     "wrong",
		TokenFile: fp,
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer conn.Close()

	if !conn.IsConnected() {
		t.Fatal("expected connected")
	}
}
