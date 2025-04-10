// Copyright 2022 The NATS Authors
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

// Package cmd is the cli entrypoint for nats-surveyor
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/nats-io/nats-surveyor/surveyor"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:     "nats-surveyor",
		Short:   "Prometheus exporter for NATS",
		RunE:    run,
		Version: Version,
	}
	logger  = logrus.New()
	Version = "v0.0.0"
)

// long flags that were introduced <=v0.2.2 originally used 'flag' package must be parsed as legacy flags
// to preserve the behavior where long flags could be supplied with a single dash
func rootCmdArgs(args []string) []string {
	// old flag: new flag
	legacyFlagMap := map[string]string{
		"-help":            "--help",
		"-version":         "--version",
		"-creds":           "--creds",
		"-nkey":            "--nkey",
		"-user":            "--user",
		"-password":        "--password",
		"-timeout":         "--timeout",
		"-port":            "--port",
		"-addr":            "--addr",
		"-tlscert":         "--tlscert",
		"-tlskey":          "--tlskey",
		"-tlscacert":       "--tlscacert",
		"-http_tlscert":    "--http-tlscert",
		"--http_tlscert":   "--http-tlscert",
		"-http_tlskey":     "--http-tlskey",
		"--http_tlskey":    "--http-tlskey",
		"-http_tlscacert":  "--http-tlscacert",
		"--http_tlscacert": "--http-tlscacert",
		"-http_user":       "--http-user",
		"--http_user":      "--http-user",
		"-http_pass":       "--http-pass",
		"--http_pass":      "--http-pass",
		"-prefix":          "--prefix",
		"-observe":         "--observe",
		"-jetstream":       "--jetstream",
		"-gatewayz":        "--gatewayz",
	}
	newArgs := make([]string, 0)

	for i, arg := range args {
		if arg == "--" {
			return append(newArgs, args[i:]...)
		}
		argSplit := strings.SplitN(arg, "=", 2)
		newArg, exists := legacyFlagMap[argSplit[0]]
		if exists {
			logger.Warnf("flag '%s' is deprecated and may be removed in a future relese, use '%s' instead", argSplit[0], newArg)
			if len(argSplit) == 1 {
				newArgs = append(newArgs, newArg)
			} else {
				newArgs = append(newArgs, fmt.Sprintf("%s=%s", newArg, argSplit[1]))
			}
		} else {
			newArgs = append(newArgs, arg)
		}
	}
	return newArgs
}

func Execute() {
	if len(os.Args) > 1 {
		rootCmd.SetArgs(rootCmdArgs(os.Args[1:]))
	}
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func initConfig() {
	viper.SetEnvPrefix("nats_surveyor")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("nats-surveyor")
		viper.AddConfigPath("/etc/nats-surveyor")
		viper.AddConfigPath(".")
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			logger.Fatalln(err)
		}
	} else {
		logger.Infof("Using config:  %s", viper.ConfigFileUsed())
	}
}

func init() {
	logger.SetFormatter(&nested.Formatter{
		TimestampFormat: time.RFC3339,
	})

	// config
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./nats-surveyor.yaml)")

	// servers
	rootCmd.Flags().StringP("servers", "s", nats.DefaultURL, "NATS Cluster url(s)")
	_ = viper.BindPFlag("servers", rootCmd.Flags().Lookup("servers"))

	// count
	rootCmd.Flags().IntP("count", "c", 1, "Expected number of servers (-1 for undefined).")
	_ = viper.BindPFlag("count", rootCmd.Flags().Lookup("count"))

	// creds
	rootCmd.Flags().String("creds", "", "Credentials File")
	_ = viper.BindPFlag("creds", rootCmd.Flags().Lookup("creds"))

	// nkey
	rootCmd.Flags().String("nkey", "", "Nkey Seed File")
	_ = viper.BindPFlag("nkey", rootCmd.Flags().Lookup("nkey"))

	// jwt
	rootCmd.Flags().String("jwt", "", "User JWT. Use in conjunction with --seed")
	_ = viper.BindPFlag("jwt", rootCmd.Flags().Lookup("jwt"))

	// seed
	rootCmd.Flags().String("seed", "", "Private key (nkey seed). Use in conjunction with --jwt")
	_ = viper.BindPFlag("seed", rootCmd.Flags().Lookup("seed"))

	// user
	rootCmd.Flags().String("user", "", "NATS user name or token")
	_ = viper.BindPFlag("user", rootCmd.Flags().Lookup("user"))

	// password
	rootCmd.Flags().String("password", "", "NATS user password")
	_ = viper.BindPFlag("password", rootCmd.Flags().Lookup("password"))

	// server-discovery-timeout
	rootCmd.Flags().DurationP("server-discovery-timeout", "", 500*time.Millisecond, "Maximum wait time between responses from servers during server discovery. Use in conjunction with -count=-1.")
	_ = viper.BindPFlag("server-discovery-timeout", rootCmd.Flags().Lookup("server-discovery-timeout"))

	// timeout
	rootCmd.Flags().Duration("timeout", surveyor.DefaultPollTimeout, "Polling timeout")
	_ = viper.BindPFlag("timeout", rootCmd.Flags().Lookup("timeout"))

	// tlscert
	rootCmd.Flags().String("tlscert", "", "Client certificate file for NATS connections.")
	_ = viper.BindPFlag("tlscert", rootCmd.Flags().Lookup("tlscert"))

	// tlskey
	rootCmd.Flags().String("tlskey", "", "Client private key for NATS connections.")
	_ = viper.BindPFlag("tlskey", rootCmd.Flags().Lookup("tlskey"))

	// tlscacert
	rootCmd.Flags().String("tlscacert", "", "Client certificate CA on NATS connections.")
	_ = viper.BindPFlag("tlscacert", rootCmd.Flags().Lookup("tlscacert"))

	// tlsfirst
	rootCmd.Flags().Bool("tlsfirst", false, "Whether to use TLS First connections.")
	_ = viper.BindPFlag("tlsfirst", rootCmd.Flags().Lookup("tlsfirst"))

	// port
	rootCmd.Flags().IntP("port", "p", surveyor.DefaultListenPort, "Port to listen on.")
	_ = viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))

	// addr
	rootCmd.Flags().StringP("addr", "a", surveyor.DefaultListenAddress, "Network host to listen on.")
	_ = viper.BindPFlag("addr", rootCmd.Flags().Lookup("addr"))

	// http-tlscert
	rootCmd.Flags().String("http-tlscert", "", "Server certificate file (Enables HTTPS).")
	_ = viper.BindPFlag("http-tlscert", rootCmd.Flags().Lookup("http-tlscert"))

	// http-tlskey
	rootCmd.Flags().String("http-tlskey", "", "Private key for server certificate (used with HTTPS).")
	_ = viper.BindPFlag("http-tlskey", rootCmd.Flags().Lookup("http-tlskey"))

	// http-tlscacert
	rootCmd.Flags().String("http-tlscacert", "", "Client certificate CA for verification (used with HTTPS).")
	_ = viper.BindPFlag("http-tlscacert", rootCmd.Flags().Lookup("http-tlscacert"))

	// http-user
	rootCmd.Flags().String("http-user", "", "Enable basic auth and set user name for HTTP scrapes.")
	_ = viper.BindPFlag("http-user", rootCmd.Flags().Lookup("http-user"))

	// http-pass
	rootCmd.Flags().String("http-pass", "", "Set the password for HTTP scrapes. NATS bcrypt supported.")
	_ = viper.BindPFlag("http-pass", rootCmd.Flags().Lookup("http-pass"))

	// prefix
	rootCmd.Flags().String("prefix", "", "Replace the default prefix for all the metrics.")
	_ = viper.BindPFlag("prefix", rootCmd.Flags().Lookup("prefix"))

	// observe
	rootCmd.Flags().String("observe", "", "Listen for observation statistics based on config files in a directory.")
	_ = viper.BindPFlag("observe", rootCmd.Flags().Lookup("observe"))

	// jetstream
	rootCmd.Flags().String("jetstream", "", "Listen for JetStream Advisories based on config files in a directory.")
	_ = viper.BindPFlag("jetstream", rootCmd.Flags().Lookup("jetstream"))

	// accounts
	rootCmd.Flags().Bool("accounts", false, "Export per account metrics")
	_ = viper.BindPFlag("accounts", rootCmd.Flags().Lookup("accounts"))

	// gatewayz
	rootCmd.Flags().Bool("gatewayz", false, "Export gateway metrics")
	_ = viper.BindPFlag("gatewayz", rootCmd.Flags().Lookup("gatewayz"))

	// sys-req-prefix
	rootCmd.Flags().String("sys-req-prefix", surveyor.DefaultSysReqPrefix, "Subject prefix for system requests ($SYS.REQ)")
	_ = viper.BindPFlag("sys-req-prefix", rootCmd.Flags().Lookup("sys-req-prefix"))

	// log-level
	rootCmd.Flags().String("log-level", "info", "Log level, one of: trace|debug|info|warn|error|fatal|panic")
	_ = viper.BindPFlag("log-level", rootCmd.Flags().Lookup("log-level"))

	rootCmd.Flags().SortFlags = false

	cobra.OnInitialize(initConfig)
}

func getSurveyorOpts() *surveyor.Options {
	opts := surveyor.GetDefaultOptions()
	opts.URLs = viper.GetString("servers")
	opts.Credentials = viper.GetString("creds")
	opts.Nkey = viper.GetString("nkey")
	opts.JWT = viper.GetString("jwt")
	opts.Seed = viper.GetString("seed")
	opts.NATSUser = viper.GetString("user")
	opts.NATSPassword = viper.GetString("password")
	opts.ExpectedServers = viper.GetInt("count")
	opts.PollTimeout = viper.GetDuration("timeout")
	opts.ListenPort = viper.GetInt("port")
	opts.ListenAddress = viper.GetString("addr")
	opts.CertFile = viper.GetString("tlscert")
	opts.KeyFile = viper.GetString("tlskey")
	opts.CaFile = viper.GetString("tlscacert")
	opts.TLSFirst = viper.GetBool("tlsfirst")
	opts.HTTPCertFile = viper.GetString("http-tlscert")
	opts.HTTPKeyFile = viper.GetString("http-tlskey")
	opts.HTTPCaFile = viper.GetString("http-tlscacert")
	opts.HTTPUser = viper.GetString("http-user")
	opts.HTTPPassword = viper.GetString("http-pass")
	opts.Prefix = viper.GetString("prefix")
	opts.ObservationConfigDir = viper.GetString("observe")
	opts.JetStreamConfigDir = viper.GetString("jetstream")
	opts.Accounts = viper.GetBool("accounts")
	opts.Gatewayz = viper.GetBool("gatewayz")
	opts.SysReqPrefix = viper.GetString("sys-req-prefix")
	opts.ServerResponseWait = viper.GetDuration("server-discovery-timeout")

	logLevel, err := logrus.ParseLevel(viper.GetString("log-level"))
	if err == nil {
		logger.SetLevel(logLevel)
	} else {
		logger.Warnln(err)
	}
	opts.Logger = logger

	return opts
}

func run(cmd *cobra.Command, args []string) error {
	opts := getSurveyorOpts()

	s, err := surveyor.NewSurveyor(opts)
	if err != nil {
		return fmt.Errorf("couldn't start surveyor: %v", err)
	}
	err = s.Start()
	if err != nil {
		return fmt.Errorf("couldn't start surveyor: %s", err)
	}

	// set up the interrupt handler to gracefully exit.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGQUIT)
	go func() {
		for {
			sig := <-c

			switch sig {
			case syscall.SIGQUIT:
				buf := make([]byte, 1<<20)
				stacklen := runtime.Stack(buf, true)
				logger.Warnln(string(buf[:stacklen]))

			default:
				s.Stop()
				os.Exit(0)
			}
		}
	}()

	runtime.Goexit()
	return nil
}
