// Copyright 2017-2018 The NATS Authors
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
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/nats-io/nats-surveyor/surveyor"
	"github.com/nats-io/nats.go"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	version = "v0.2.3-0"
)

var rootCmd = &cobra.Command{
	Use:   "surveyor",
	Short: "Prometheus exporter for NATS",
	RunE:  run,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func initConfig() {
	viper.SetEnvPrefix("surveyor")
	viper.AutomaticEnv()

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}

	viper.SetConfigName("nats-surveyor")
	viper.AddConfigPath("/etc/nats-surveyor")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err == nil {
		log.Printf("Using config:  %s\n", viper.ConfigFileUsed())
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./nats-surveyor.yaml)")

	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.Flags().Bool("version", false, "Show exporter version and exit.")
	viper.BindPFlag("version", rootCmd.Flags().Lookup("version"))
	rootCmd.Flags().StringP("servers", "s", nats.DefaultURL, "NATS Cluster url(s)")
	viper.BindPFlag("servers", rootCmd.Flags().Lookup("servers"))
	rootCmd.Flags().StringP("creds", "c", "", "Credentials File")
	viper.BindPFlag("creds", rootCmd.Flags().Lookup("creds"))
	rootCmd.Flags().String("nkey", "", "Nkey Seed File")
	viper.BindPFlag("nkey", rootCmd.Flags().Lookup("nkey"))
	rootCmd.Flags().String("user", "", "NATS user name or token")
	viper.BindPFlag("user", rootCmd.Flags().Lookup("user"))
	rootCmd.Flags().String("password", "", "NATS user password")
	viper.BindPFlag("password", rootCmd.Flags().Lookup("password"))
	rootCmd.Flags().Int("c", 1, "Expected number of servers")
	viper.BindPFlag("c", rootCmd.Flags().Lookup("c"))
	rootCmd.Flags().Duration("timeout", 3*time.Second, "Polling timeout")
	viper.BindPFlag("timeout", rootCmd.Flags().Lookup("timeout"))
	rootCmd.Flags().IntP("port", "p", surveyor.DefaultListenPort, "Port to listen on.")
	viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))
	rootCmd.Flags().StringP("addr", "a", surveyor.DefaultListenAddress, "Network host to listen on.")
	viper.BindPFlag("addr", rootCmd.Flags().Lookup("addr"))
	rootCmd.Flags().String("tlscert", "", "Client certificate file for NATS connections.")
	viper.BindPFlag("tlscert", rootCmd.Flags().Lookup("tlscert"))
	rootCmd.Flags().String("tlskey", "", "Client private key for NATS connections.")
	viper.BindPFlag("tlskey", rootCmd.Flags().Lookup("tlskey"))
	rootCmd.Flags().String("tlscacert", "", "Client certificate CA on NATS connecctions.")
	viper.BindPFlag("tlscacert", rootCmd.Flags().Lookup("tlscacert"))
	rootCmd.Flags().String("http_tlscert", "", "Server certificate file (Enables HTTPS).")
	viper.BindPFlag("http_tlscert", rootCmd.Flags().Lookup("http_tlscert"))
	rootCmd.Flags().String("http_tlskey", "", "Private key for server certificate (used with HTTPS).")
	viper.BindPFlag("http_tlskey", rootCmd.Flags().Lookup("http_tlskey"))
	rootCmd.Flags().String("http_tlscacert", "", "Client certificate CA for verification (used with HTTPS).")
	viper.BindPFlag("http_tlscacert", rootCmd.Flags().Lookup("http_tlscacert"))
	rootCmd.Flags().String("http_user", "", "Enable basic auth and set user name for HTTP scrapes.")
	viper.BindPFlag("http_user", rootCmd.Flags().Lookup("http_user"))
	rootCmd.Flags().String("http_pass", "", "Set the password for HTTP scrapes. NATS bcrypt supported.")
	viper.BindPFlag("http_pass", rootCmd.Flags().Lookup("http_pass"))
	rootCmd.Flags().String("prefix", "", "Replace the default prefix for all the metrics.")
	viper.BindPFlag("prefix", rootCmd.Flags().Lookup("prefix"))
	rootCmd.Flags().String("observe", "", "Listen for observation statistics based on config files in a directory.")
	viper.BindPFlag("observe", rootCmd.Flags().Lookup("observe"))
	rootCmd.Flags().String("jetstream", "", "Listen for JetStream Advisories based on config files in a directory.")
	viper.BindPFlag("jetstream", rootCmd.Flags().Lookup("jetstream"))
	rootCmd.Flags().Bool("accounts", false, "Export per account metrics")
	viper.BindPFlag("accounts", rootCmd.Flags().Lookup("accounts"))
	cobra.OnInitialize(initConfig)

}

func getSurveyorOpts() *surveyor.Options {
	opts := surveyor.GetDefaultOptions()
	opts.URLs = viper.GetString("servers")
	opts.Credentials = viper.GetString("creds")
	opts.Nkey = viper.GetString("nkey")
	opts.NATSUser = viper.GetString("user")
	opts.NATSPassword = viper.GetString("password")
	opts.ExpectedServers = viper.GetInt("c")
	opts.PollTimeout = viper.GetDuration("timeout")
	opts.ListenPort = viper.GetInt("port")
	opts.ListenAddress = viper.GetString("addr")
	opts.CertFile = viper.GetString("tlscert")
	opts.KeyFile = viper.GetString("tlskey")
	opts.CaFile = viper.GetString("tlscacert")
	opts.HTTPCertFile = viper.GetString("http_tlscert")
	opts.HTTPKeyFile = viper.GetString("http_tlskey")
	opts.HTTPCaFile = viper.GetString("http_tlscacert")
	opts.HTTPUser = viper.GetString("http_user")
	opts.HTTPPassword = viper.GetString("http_password")
	opts.Prefix = viper.GetString("prefix")
	opts.ObservationConfigDir = viper.GetString("observe")
	opts.JetStreamConfigDir = viper.GetString("jetstream")
	opts.Accounts = viper.GetBool("accounts")

	return opts
}

func run(cmd *cobra.Command, args []string) error {
	opts := getSurveyorOpts()

	if viper.GetBool("version") {
		fmt.Println("nats-surveyor", version)
		os.Exit(0)
	}

	s, err := surveyor.NewSurveyor(opts)
	if err != nil {
		return fmt.Errorf("couldn't start surveyor: %v", err)
	}
	err = s.Start()
	if err != nil {
		return fmt.Errorf("couldn't start surveyor: %s", err)
	}

	// Setup the interrupt handler to gracefully exit.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGQUIT)
	go func() {
		for {
			sig := <-c

			switch sig {
			case syscall.SIGQUIT:
				buf := make([]byte, 1<<20)
				stacklen := runtime.Stack(buf, true)
				fmt.Fprintln(os.Stderr, string(buf[:stacklen]))

			default:
				s.Stop()
				os.Exit(0)
			}
		}
	}()

	runtime.Goexit()

	return nil
}
