// Package main implements attestation and request services.
package main

import (
	"context"
	"flag"
	"log"
	"mainstay/attestation"
	"mainstay/clients"
	"mainstay/config"
	"mainstay/server"
	"mainstay/test"
	"os"
	"os/signal"
	"sync"
)

var (
	tx0        string
	pk0        string
	script     string
	isRegtest  bool
	apiHost    string
	mainConfig *config.Config
	ocean      clients.SidechainClient
)

func parseFlags() {
	flag.BoolVar(&isRegtest, "regtest", false, "Use regtest wallet configuration instead of user wallet")
	flag.StringVar(&tx0, "tx", "", "Tx id for genesis attestation transaction")
	flag.StringVar(&pk0, "pk", "", "Main client pk for genesis attestation transaction")
	flag.StringVar(&script, "script", "", "Redeem script in case multisig is used")
	flag.Parse()

	if (tx0 == "" || pk0 == "") && !isRegtest {
		flag.PrintDefaults()
		log.Fatalf("Need to provide both -tx and -pk argument. To use test configuration set the -regtest flag.")
	}
}

func init() {
	parseFlags()

	if isRegtest {
		test := test.NewTest(true, true)
		mainConfig = test.Config
		ocean = test.OceanClient
		log.Printf("Running regtest mode with -tx=%s\n", mainConfig.InitTX())
	} else {
		mainConfig = config.NewConfig()
		mainConfig.SetInitTX(tx0)
		mainConfig.SetInitPK(pk0)
		mainConfig.SetMultisigScript(script)
		ocean = config.NewClientFromConfig(false)
	}
}

func main() {
	defer mainConfig.MainClient().Shutdown()
	defer ocean.Close()

	wg := &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())

	server := server.NewServer(mainConfig, ocean)
	attestService := attestation.NewAttestService(ctx, wg, server, mainConfig)

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)

	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		select {
		case sig := <-c:
			log.Printf("Got %s signal. Aborting...\n", sig)
		case <-ctx.Done():
			signal.Stop(c)
		}
	}()

	wg.Add(1)
	go attestService.Run()

	if isRegtest { // In regtest demo mode do block generation work
		wg.Add(1)
		go test.DoRegtestWork(mainConfig, wg, ctx)
	}
	wg.Wait()
}
