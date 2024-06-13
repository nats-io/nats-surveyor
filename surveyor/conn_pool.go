package surveyor

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

type pooledNatsConn struct {
	nc     *nats.Conn
	cp     *natsConnPool
	key    string
	count  uint64
	closed bool
}

func (pc *pooledNatsConn) ReturnToPool() {
	pc.cp.Lock()
	pc.count--
	if pc.count == 0 {
		if pooledConn, ok := pc.cp.cache[pc.key]; ok && pc == pooledConn {
			delete(pc.cp.cache, pc.key)
		}
		pc.closed = true
		pc.cp.Unlock()
		pc.nc.Close()
		return
	}
	pc.cp.Unlock()
}

type natsConnPool struct {
	sync.Mutex
	cache        map[string]*pooledNatsConn
	logger       *logrus.Logger
	group        *singleflight.Group
	natsDefaults []nats.Option
	natsOpts     []nats.Option
}

func newNatsConnPool(logger *logrus.Logger, natsDefaults []nats.Option, natsOpts []nats.Option) *natsConnPool {
	return &natsConnPool{
		cache:        map[string]*pooledNatsConn{},
		logger:       logger,
		group:        &singleflight.Group{},
		natsDefaults: natsDefaults,
		natsOpts:     natsOpts,
	}
}

const getPooledConnMaxTries = 10

// Get returns a *pooledNatsConn
func (cp *natsConnPool) Get(opts []nats.Option) (*pooledNatsConn, error) {
	if len(opts) == 0 {
		return nil, fmt.Errorf("nats options must not be empty ")
	}

	key := cp.hash(opts)

	for i := 0; i < getPooledConnMaxTries; i++ {
		connection, err := cp.getPooledConn(key, opts)
		if err != nil {
			return nil, err
		}

		cp.Lock()
		if connection.closed {
			// ReturnToPool closed this while lock not held, try again
			cp.Unlock()
			continue
		}

		// increment count out of the pool
		connection.count++
		cp.Unlock()
		return connection, nil
	}

	return nil, fmt.Errorf("failed to get pooled connection after %d attempts", getPooledConnMaxTries)
}

// getPooledConn gets or establishes a *pooledNatsConn in a singleflight group, but does not increment its count
func (cp *natsConnPool) getPooledConn(key string, opts []nats.Option) (*pooledNatsConn, error) {
	conn, err, _ := cp.group.Do(key, func() (interface{}, error) {
		cp.Lock()
		pooledConn, ok := cp.cache[key]
		if ok && pooledConn.nc.IsConnected() {
			cp.Unlock()
			return pooledConn, nil
		}
		cp.Unlock()

		connOpts := nats.GetDefaultOptions()
		for _, o := range opts {
			o(&connOpts)
		}

		nc, err := connOpts.Connect()
		if err != nil {
			return nil, err
		}
		cp.logger.Infof("%s connected to NATS Deployment: %s", connOpts.Name, nc.ConnectedAddr())

		connection := &pooledNatsConn{
			nc:  nc,
			cp:  cp,
			key: key,
		}

		cp.Lock()
		cp.cache[key] = connection
		cp.Unlock()

		return connection, err
	})

	if err != nil {
		return nil, err
	}

	connection, ok := conn.(*pooledNatsConn)
	if !ok {
		return nil, fmt.Errorf("not a pooledNatsConn")
	}
	return connection, nil
}

func (cp *natsConnPool) hash(opts []nats.Option) string {
	cloneOpts := nats.GetDefaultOptions()

	// Set opts
	for _, o := range opts {
		o(&cloneOpts)
	}

	ptrBytes := make([]byte, 0)
	for _, f := range opts {
		ptrBytes = append(ptrBytes, []byte(fmt.Sprintf("%p", f))...)
	}
	hash := sha256.New()
	hash.Write(ptrBytes)

	return fmt.Sprintf("%x", hash.Sum(nil))
}
