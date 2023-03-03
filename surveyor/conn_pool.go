package surveyor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
	"os"
	"sync"
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

	{
		// copy cfg
		cfgCp := &natsContext{}
		*cfgCp = *cfg
		cfg = cfgCp
	}

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

	b, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling context to json: %v", err)
	}
	if cfg.Nkey != "" {
		fb, err := os.ReadFile(cfg.Nkey)
		if err != nil {
			return nil, fmt.Errorf("error opening nkey file %s: %v", cfg.Nkey, err)
		}
		b = append(b, fb...)
	}
	if cfg.Credentials != "" {
		fb, err := os.ReadFile(cfg.Credentials)
		if err != nil {
			return nil, fmt.Errorf("error opening creds file %s: %v", cfg.Credentials, err)
		}
		b = append(b, fb...)
	}
	if cfg.TLSCA != "" {
		fb, err := os.ReadFile(cfg.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("error opening ca file %s: %v", cfg.TLSCA, err)
		}
		b = append(b, fb...)
	}
	if cfg.TLSCert != "" {
		fb, err := os.ReadFile(cfg.TLSCert)
		if err != nil {
			return nil, fmt.Errorf("error opening cert file %s: %v", cfg.TLSCert, err)
		}
		b = append(b, fb...)
	}
	if cfg.TLSKey != "" {
		fb, err := os.ReadFile(cfg.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("error opening key file %s: %v", cfg.TLSKey, err)
		}
		b = append(b, fb...)
	}
	hash := sha256.New()
	hash.Write(b)
	key := fmt.Sprintf("%x", hash.Sum(nil))

GetFromPool:
	conn, err, _ := cp.group.Do(key, func() (interface{}, error) {
		cp.Lock()
		pooledConn, ok := cp.cache[key]
		if ok && pooledConn.nc.IsConnected() {
			cp.Unlock()
			return pooledConn, nil
		}
		cp.Unlock()

		opts := cp.natsOpts[:]
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
