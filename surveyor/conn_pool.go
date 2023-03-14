package surveyor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

type natsContext struct {
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
}

func (c *natsContext) copy() *natsContext {
	if c == nil {
		return nil
	}
	cp := *c
	return &cp
}

func (c *natsContext) hash() (string, error) {
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
	Name    string
	URL     string
	TLSCA   string
	TLSCert string
	TLSKey  string
}

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

func (cp *natsConnPool) Get(cfg *natsContext) (*pooledNatsConn, error) {
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

GetFromPool:
	conn, err, _ := cp.group.Do(key, func() (interface{}, error) {
		cp.Lock()
		pooledConn, ok := cp.cache[key]
		if ok && pooledConn.nc.IsConnected() {
			cp.Unlock()
			return pooledConn, nil
		}
		cp.Unlock()

		opts := cp.natsOpts
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

	cp.Lock()
	if connection.closed {
		// ReturnToPool closed this while lock not held, try again
		cp.Unlock()
		goto GetFromPool
	}
	// increment count out of the pool
	connection.count++
	cp.Unlock()

	return connection, nil
}
