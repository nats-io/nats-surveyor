package surveyor

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

type ConnProvider interface {
	Get(*NatsContext) (Conn, error)
	Close(bool)
}

type Conn interface {
	Conn() *nats.Conn
	Close()
	IsConnected() bool
	IsClosed() bool
}

type NatsContext struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	JWT         string `json:"jwt"`
	Seed        string `json:"seed"`
	Credentials string `json:"credential"`
	Nkey        string `json:"nkey"`
	Token       string `json:"token"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	TLSCA       string `json:"tls_ca"`
	TLSCert     string `json:"tls_cert"`
	TLSKey      string `json:"tls_key"`

	// only passed programmatically
	NatsOptsID string        `json:"nats_opts_id"`
	NatsOpts   []nats.Option `json:"-"`
}

func (c *NatsContext) copy() *NatsContext {
	if c == nil {
		return nil
	}
	cp := *c
	return &cp
}

func (c *NatsContext) hash() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("error marshaling context to json: %v", err)
	}
	if c.Nkey != "" {
		fb, err := os.ReadFile(c.Nkey)
		if err != nil {
			return "", fmt.Errorf("error opening nkey file %s: %v", c.Nkey, err)
		}
		b = append(b, fb...)
	}
	if c.Credentials != "" {
		fb, err := os.ReadFile(c.Credentials)
		if err != nil {
			return "", fmt.Errorf("error opening creds file %s: %v", c.Credentials, err)
		}
		b = append(b, fb...)
	}
	if c.TLSCA != "" {
		fb, err := os.ReadFile(c.TLSCA)
		if err != nil {
			return "", fmt.Errorf("error opening ca file %s: %v", c.TLSCA, err)
		}
		b = append(b, fb...)
	}
	if c.TLSCert != "" {
		fb, err := os.ReadFile(c.TLSCert)
		if err != nil {
			return "", fmt.Errorf("error opening cert file %s: %v", c.TLSCert, err)
		}
		b = append(b, fb...)
	}
	if c.TLSKey != "" {
		fb, err := os.ReadFile(c.TLSKey)
		if err != nil {
			return "", fmt.Errorf("error opening key file %s: %v", c.TLSKey, err)
		}
		b = append(b, fb...)
	}
	hash := sha256.New()
	hash.Write(b)
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

type natsContextDefaults struct {
	Name      string
	URL       string
	TLSCA     string
	TLSCert   string
	TLSKey    string
	TLSConfig *tls.Config
}

type pooledNatsConn struct {
	nc     *nats.Conn
	cp     *natsConnPool
	key    string
	count  uint64
	closed bool
}

func (pc *pooledNatsConn) Conn() *nats.Conn {
	return pc.nc
}

func (pc *pooledNatsConn) Close() {
	pc.ReturnToPool()
}

func (pc *pooledNatsConn) IsConnected() bool {
	return pc.nc.IsConnected()
}

func (pc *pooledNatsConn) IsClosed() bool {
	return pc.nc.IsClosed()
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
	openConns    []*pooledNatsConn
	logger       *logrus.Logger
	group        *singleflight.Group
	natsDefaults *natsContextDefaults
	natsOpts     []nats.Option
}

func newNatsConnPool(logger *logrus.Logger, natsDefaults *natsContextDefaults, natsOpts []nats.Option) *natsConnPool {
	return &natsConnPool{
		cache:        map[string]*pooledNatsConn{},
		group:        &singleflight.Group{},
		logger:       logger,
		natsDefaults: natsDefaults,
		natsOpts:     natsOpts,
	}
}

const getPooledConnMaxTries = 10

func (cp *natsConnPool) Close(force bool) {
	cp.Mutex.Lock()
	defer cp.Mutex.Lock()

	for _, c := range cp.openConns {
		if force {
			c.nc.Close()
		} else {
			c.Close()
		}
	}
}

// Get returns a *pooledNatsConn
func (cp *natsConnPool) Get(cfg *NatsContext) (Conn, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nats context must not be nil")
	}

	// copy cfg
	cfg = cfg.copy()

	// set defaults
	if cfg.Name == "" {
		cfg.Name = cp.natsDefaults.Name
	}
	if cfg.URL == "" {
		cfg.URL = cp.natsDefaults.URL
	}
	if cfg.TLSCA == "" {
		cfg.TLSCA = cp.natsDefaults.TLSCA
	}
	if cfg.TLSCert == "" {
		cfg.TLSCert = cp.natsDefaults.TLSCert
	}
	if cfg.TLSKey == "" {
		cfg.TLSKey = cp.natsDefaults.TLSKey
	}

	// get hash
	key, err := cfg.hash()
	if err != nil {
		return nil, err
	}

	for range getPooledConnMaxTries {
		connection, err := cp.getPooledConn(key, cfg)
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
func (cp *natsConnPool) getPooledConn(key string, cfg *NatsContext) (*pooledNatsConn, error) {
	conn, err, _ := cp.group.Do(key, func() (interface{}, error) {
		cp.Lock()
		pooledConn, ok := cp.cache[key]
		if ok {
			if pooledConn.IsConnected() {
				cp.Unlock()
				return pooledConn, nil
			}
			if !pooledConn.IsClosed() {
				openConnections := []*pooledNatsConn{pooledConn}
				for _, c := range cp.openConns {
					if !c.IsClosed() {
						openConnections = append(openConnections, c)
					}
					cp.openConns = openConnections
				}
			}
		}
		cp.Unlock()

		opts := append(cp.natsOpts, cfg.NatsOpts...)
		opts = append(opts, func(options *nats.Options) error {
			if cfg.Name != "" {
				options.Name = cfg.Name
			}
			if cfg.Token != "" {
				options.Token = cfg.Token
			}
			if cfg.Username != "" {
				options.User = cfg.Username
			}
			if cfg.Password != "" {
				options.Password = cfg.Password
			}
			return nil
		})

		if cfg.JWT != "" && cfg.Seed != "" {
			opts = append(opts, nats.UserJWTAndSeed(cfg.JWT, cfg.Seed))
		}

		if cfg.Nkey != "" {
			opt, err := nats.NkeyOptionFromSeed(cfg.Nkey)
			if err != nil {
				return nil, fmt.Errorf("unable to load nkey: %v", err)
			}
			opts = append(opts, opt)
		}

		if cfg.Credentials != "" {
			opts = append(opts, nats.UserCredentials(cfg.Credentials))
		}

		if cfg.TLSCA != "" {
			opts = append(opts, nats.RootCAs(cfg.TLSCA))
		}

		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			opts = append(opts, nats.ClientCert(cfg.TLSCert, cfg.TLSKey))
		}

		nc, err := nats.Connect(cfg.URL, opts...)
		if err != nil {
			return nil, err
		}
		cp.logger.Infof("%s connected to NATS Deployment: %s", cfg.Name, nc.ConnectedAddr())

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
