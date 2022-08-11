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
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/nats-io/nats-surveyor/surveyor"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:     "nats-surveyor",
		Short:   "Prometheus exporter for NATS",
		RunE:    run,
		Version: "v0.3.0",
	}
)

// long flags that introduced <=v0.2.2 originally used 'flag' package must be parsed as legacy flags
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
		"-observe":         "-observe",
		"-jetstream":       "-jetstream",
	}
	newArgs := make([]string, 0)

	for i, arg := range args {
		if arg == "--" {
			return append(newArgs, args[i:]...)
		}
		argSplit := strings.SplitN(arg, "=", 2)
		newArg, exists := legacyFlagMap[argSplit[0]]
		if exists {
			log.Printf("flag '%s' is deprecated and may be removed in a future relese, use '%s' instead", argSplit[0], newArg)
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
			log.Println(err)
			os.Exit(1)
		}
	} else {
		log.Printf("Using config:  %s\n", viper.ConfigFileUsed())
	}
}

func init() {
	// config
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./nats-surveyor.yaml)")

	// servers
	rootCmd.Flags().StringP("servers", "s", nats.DefaultURL, "NATS Cluster url(s)")
	viper.BindPFlag("servers", rootCmd.Flags().Lookup("servers"))

	// creds
	rootCmd.Flags().String("creds", "", "Credentials File")
	viper.BindPFlag("creds", rootCmd.Flags().Lookup("creds"))

	// nkey
	rootCmd.Flags().String("nkey", "", "Nkey Seed File")
	viper.BindPFlag("nkey", rootCmd.Flags().Lookup("nkey"))

	// user
	rootCmd.Flags().String("user", "", "NATS user name or token")
	viper.BindPFlag("user", rootCmd.Flags().Lookup("user"))

	// password
	rootCmd.Flags().String("password", "", "NATS user password")
	viper.BindPFlag("password", rootCmd.Flags().Lookup("password"))

	// count
	rootCmd.Flags().IntP("count", "c", 1, "Expected number of servers")
	viper.BindPFlag("count", rootCmd.Flags().Lookup("count"))

	// timeout
	rootCmd.Flags().Duration("timeout", surveyor.DefaultPollTimeout, "Polling timeout")
	viper.BindPFlag("timeout", rootCmd.Flags().Lookup("timeout"))

	// port
	rootCmd.Flags().IntP("port", "p", surveyor.DefaultListenPort, "Port to listen on.")
	viper.BindPFlag("port", rootCmd.Flags().Lookup("port"))

	// addr
	rootCmd.Flags().StringP("addr", "a", surveyor.DefaultListenAddress, "Network host to listen on.")
	viper.BindPFlag("addr", rootCmd.Flags().Lookup("addr"))

	// tlscert
	rootCmd.Flags().String("tlscert", "", "Client certificate file for NATS connections.")
	viper.BindPFlag("tlscert", rootCmd.Flags().Lookup("tlscert"))

	// tlskey
	rootCmd.Flags().String("tlskey", "", "Client private key for NATS connections.")
	viper.BindPFlag("tlskey", rootCmd.Flags().Lookup("tlskey"))

	// tlscacert
	rootCmd.Flags().String("tlscacert", "", "Client certificate CA on NATS connecctions.")
	viper.BindPFlag("tlscacert", rootCmd.Flags().Lookup("tlscacert"))

	// http-tlscert
	rootCmd.Flags().String("http-tlscert", "", "Server certificate file (Enables HTTPS).")
	viper.BindPFlag("http-tlscert", rootCmd.Flags().Lookup("http-tlscert"))

	// http-tlskey
	rootCmd.Flags().String("http-tlskey", "", "Private key for server certificate (used with HTTPS).")
	viper.BindPFlag("http-tlskey", rootCmd.Flags().Lookup("http-tlskey"))

	// http-tlscacert
	rootCmd.Flags().String("http-tlscacert", "", "Client certificate CA for verification (used with HTTPS).")
	viper.BindPFlag("http-tlscacert", rootCmd.Flags().Lookup("http-tlscacert"))

	// http-user
	rootCmd.Flags().String("http-user", "", "Enable basic auth and set user name for HTTP scrapes.")
	viper.BindPFlag("http-user", rootCmd.Flags().Lookup("http-user"))

	// http-pass
	rootCmd.Flags().String("http-pass", "", "Set the password for HTTP scrapes. NATS bcrypt supported.")
	viper.BindPFlag("http-pass", rootCmd.Flags().Lookup("http-pass"))

	// prefix
	rootCmd.Flags().String("prefix", "", "Replace the default prefix for all the metrics.")
	viper.BindPFlag("prefix", rootCmd.Flags().Lookup("prefix"))

	// observe
	rootCmd.Flags().String("observe", "", "Listen for observation statistics based on config files in a directory.")
	viper.BindPFlag("observe", rootCmd.Flags().Lookup("observe"))

	// jetstream
	rootCmd.Flags().String("jetstream", "", "Listen for JetStream Advisories based on config files in a directory.")
	viper.BindPFlag("jetstream", rootCmd.Flags().Lookup("jetstream"))

	// accounts
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
	opts.ExpectedServers = viper.GetInt("count")
	opts.PollTimeout = viper.GetDuration("timeout")
	opts.ListenPort = viper.GetInt("port")
	opts.ListenAddress = viper.GetString("addr")
	opts.CertFile = viper.GetString("tlscert")
	opts.KeyFile = viper.GetString("tlskey")
	opts.CaFile = viper.GetString("tlscacert")
	opts.HTTPCertFile = viper.GetString("http-tlscert")
	opts.HTTPKeyFile = viper.GetString("http-tlskey")
	opts.HTTPCaFile = viper.GetString("http-tlscacert")
	opts.HTTPUser = viper.GetString("http-user")
	opts.HTTPPassword = viper.GetString("http-pass")
	opts.Prefix = viper.GetString("prefix")
	opts.ObservationConfigDir = viper.GetString("observe")
	opts.JetStreamConfigDir = viper.GetString("jetstream")
	opts.Accounts = viper.GetBool("accounts")

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
