package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	abciserver "github.com/tendermint/tendermint/abci/server"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmrpcserver "github.com/tendermint/tendermint/rpc/jsonrpc/server"

	"github.com/smartbch/ganychain/app"
	"github.com/smartbch/ganychain/backend"
	"github.com/smartbch/ganychain/follower"
	"github.com/smartbch/ganychain/rpc"
	"github.com/smartbch/ganychain/web3client"
)

const (
	BadgerGCInterval   = time.Second * 600
	BadgerDiscardRatio = 0.5

	DBPathTemplate       = "./tmp/shard%v/badger"
	TendermintConfigPath = "./tmp/config/config.toml"
	GanyConfigPath       = "./config"
	FollowerHomePath     = "./tmp/follower"
)

var (
	// global flags
	flagAbci         string
	flagVerbose      bool   // for the println output
	flagConfigPath   string // for the config file path
	flagFollowerHome string // for the follower home

	// rpc config
	flagRpcHttpAddr  string
	flagRpcHttpsAddr string
	flagRpcHttpApi   string
	flagSbchRpcAddr  string
	flagSbchWsAddr   string

	// tendermint node config
	numOfShards int // number of shards
	shardPorts  []string

	// socket server config
	serverPorts []string
)

var RootCmd = &cobra.Command{
	Use:   "gany",
	Short: "ganychain",
	Long:  "GanyChain：a satellite chain for smartBCH",
}

func Execute() error {
	readConfig()
	addGlobalFlags()
	addCommands()
	return RootCmd.Execute()
}

func readConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(GanyConfigPath)
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		os.Exit(1)
	}

	numOfShards = viper.GetInt("tendermint.shards")
	shardPorts = viper.GetStringSlice("tendermint.shard-ports")
	serverPorts = viper.GetStringSlice("server.ports")

	if numOfShards != len(serverPorts) || numOfShards != len(shardPorts) {
		fmt.Fprintf(os.Stderr, "length of server ports(%d) or length of rpc ports(%d) is not equal to numOfShards(%d)\n",
			len(serverPorts), len(shardPorts), numOfShards)
		os.Exit(1)
	}

	flagRpcHttpAddr = viper.GetString("rpc.http-addr")
	flagRpcHttpsAddr = viper.GetString("rpc.https-addr")
	flagRpcHttpApi = viper.GetString("rpc.http-api")

	flagSbchRpcAddr = viper.GetString("follower.smartbch-rpc-url")
	flagSbchWsAddr = viper.GetString("follower.smartbch-ws-url")
}

func addGlobalFlags() {
	RootCmd.PersistentFlags().StringVarP(&flagAbci, "abci", "", "socket", "either socket or grpc")
	RootCmd.PersistentFlags().BoolVarP(&flagVerbose,
		"verbose",
		"v",
		false,
		"print the command and results as if it were a console session")
	RootCmd.PersistentFlags().StringVarP(&flagConfigPath, "config", "", TendermintConfigPath, "path to config.toml")
	RootCmd.PersistentFlags().StringVarP(&flagFollowerHome, "follower-home", "", FollowerHomePath, "path to follower home")
}

func addCommands() {
	RootCmd.AddCommand(startCmd)
	RootCmd.AddCommand(followerCmd)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "start ganychain",
	Long:  "start ganychain",
	Args:  cobra.ExactArgs(0),
	RunE:  cmdGanyApp,
}

var followerCmd = &cobra.Command{
	Use:   "init",
	Short: "init follower config",
	Long:  "init follower config",
	Args:  cobra.ExactArgs(0),
	RunE:  cmdInitFollower,
}

//--------------------------------------------------------------------------------

func cmdInitFollower(cmd *cobra.Command, args []string) error {
	ensureRoot(flagFollowerHome)
	return nil
}

func ensureRoot(home string) {
	const DefaultDirPerm = 0700
	if err := tmos.EnsureDir(home, DefaultDirPerm); err != nil {
		panic(err.Error())
	}
	if err := tmos.EnsureDir(filepath.Join(home, "config"), DefaultDirPerm); err != nil {
		panic(err.Error())
	}
	if err := tmos.EnsureDir(filepath.Join(home, "data"), DefaultDirPerm); err != nil {
		panic(err.Error())
	}
}

func cmdGanyApp(cmd *cobra.Command, args []string) error {
	logger := tmlog.MustNewDefaultLogger(tmlog.LogFormatPlain, tmlog.LogLevelInfo, false)

	apps := make([]app.GanyApp, numOfShards)
	dbs := make([]*badger.DB, numOfShards)
	ctx, cancel := context.WithCancel(context.Background())

	sbchClient := web3client.NewSbchClient(flagSbchRpcAddr)
	followerConfig := follower.DefaultConfig(flagFollowerHome, flagSbchRpcAddr, flagSbchWsAddr)
	follower := follower.NewSbchFollower(followerConfig, logger, sbchClient)

	for i, tmPort := range shardPorts {
		dbPath := fmt.Sprintf(DBPathTemplate, i)
		db, err := badger.Open(badger.DefaultOptions(dbPath))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open badger db: %v\n", err)
			os.Exit(1)
		}

		dbs[i] = db
		apps[i] = app.NewGanyApplication(db, tmPort, logger.With("module", "gany-app", "shard", i))
		go startNewListener(ctx, apps[i], serverPorts[i], flagAbci, logger.With("module", "abci-server", "shard", i))
		go runBadgerGC(db, logger.With("module", "badger-db", "shard", i))
	}

	go startRpcServer(ctx, apps, follower, sbchClient, flagRpcHttpAddr, "", flagRpcHttpsAddr, "",
		"*", "", "", logger.With("module", "rpc-server"), flagRpcHttpApi, "")

	defer closeDbs(dbs)

	// Stop upon receiving SIGTERM or CTRL-C.
	tmos.TrapSignal(logger, func() {
		cancel()
		logger.Info("Stopping server...")
	})

	// Run forever.
	select {}
}

func closeDbs(dbs []*badger.DB) {
	for _, d := range dbs {
		d.Close()
	}
}

func startNewListener(ctx context.Context, app abcitypes.Application, serverPort, transport string, logger tmlog.Logger) {
	addr := fmt.Sprintf("tcp://0.0.0.0:%v", serverPort)

	srv, err := abciserver.NewServer(addr, transport, app)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create server listener failed. err: %v\n", err)
		os.Exit(1)
		return
	}

	srv.SetLogger(logger.With("address", addr, "transport", transport))
	if err := srv.Start(); err != nil {
		logger.Error("Error while start socket server", "err", err)
		return
	}

	select {
	case <-ctx.Done():
		err := srv.Stop()
		if err != nil {
			logger.Error("Error while stopping socket server", "err", err)
			return
		}
	}
}

func startRpcServer(ctx context.Context, apps []app.GanyApp, followerApp follower.FollowerService,
	sbchClient web3client.Web3Client, rpcAddr, wsAddr,
	rpcAddrSecure, wsAddrSecure, corsDomain, certFile, keyFile string, logger tmlog.Logger, httpAPI, wsAPI string) {

	serverCfg := tmrpcserver.DefaultConfig()
	rpcBackend := backend.NewBackend(apps, followerApp, sbchClient)

	rpcServer := rpc.NewServer(rpcAddr, wsAddr, rpcAddrSecure, wsAddrSecure, corsDomain, certFile, keyFile,
		serverCfg, rpcBackend, logger, httpAPI, wsAPI)

	if err := rpcServer.Start(); err != nil {
		logger.Error("Error while start rpc server", "err", err)
		return
	}

	select {
	case <-ctx.Done():
		err := rpcServer.Stop()
		if err != nil {
			logger.Error("Error while stopping rpc server", "err", err)
			return
		}
	}
}

//--------------------------------------------------------------------------------

func runBadgerGC(db *badger.DB, logger tmlog.Logger) {
	ticker := time.NewTicker(BadgerGCInterval)
	for {
		select {
		case <-ticker.C:
			err := db.RunValueLogGC(BadgerDiscardRatio)
			if err != nil {
				if err == badger.ErrNoRewrite {
					logger.Error("no BadgerDB GC occurred.", "err", err)
				} else {
					logger.Error("failed to GC BadgerDB.", "err", err)
				}
			}
		}
	}
}
