package surveyor

import (
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

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
