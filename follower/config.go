package follower

import (
	"os"
	"path/filepath"
)

const (
	DefaultRpcEthGetLogsMaxResults = 10000
	DefaultNumKeptBlocks           = 10000
	DefaultNumKeptBlocksInMoDB     = -1
	DefaultTrunkCacheSize          = 200
	DefaultPruneEveryN             = 10

	AppDataPath  = "app"
	ModbDataPath = "modb"
)

type AppConfig struct {
	RootPath        string `mapstructure:"root_path"`
	GenesisFilePath string `mapstructure:"genesis_file_path"`
	//app config:
	AppDataPath  string `mapstructure:"app_data_path"`
	ModbDataPath string `mapstructure:"modb_data_path"`
	// rpc config
	RpcEthGetLogsMaxResults int `mapstructure:"get_logs_max_results"`
	// Use LiteDB instead of MoDB
	UseLiteDB bool `mapstructure:"use_litedb"`
	// the number of kept recent blocks for moeingads
	NumKeptBlocks int64 `mapstructure:"blocks_kept_ads"`
	// the number of kept recent blocks for moeingdb
	NumKeptBlocksInMoDB int64 `mapstructure:"blocks_kept_modb"`
	// the entry count of the signature cache
	TrunkCacheSize int    `mapstructure:"trunk_cache_size"`
	PruneEveryN    int64  `mapstructure:"prune_every_n"`
	SmartBchRPCUrl string `mapstructure:"smartbch-rpc-url"`
	SmartBchWsUrl  string `mapstructure:"smartbch-ws-url"`

	ArchiveMode bool `mapstructure:"archive-mode"`
	// Output level for logging
	LogLevel string `mapstructure:"log_level"`
}

type ChainConfig struct {
	*AppConfig `mapstructure:"app_config"`
}

func DefaultConfig(home, sbchRpcUrl, sbchWsUrl string) *ChainConfig {
	c := &ChainConfig{
		AppConfig: DefaultAppConfig(home, sbchRpcUrl, sbchWsUrl),
	}
	return c
}

func DefaultAppConfig(home, sbchRpcUrl, sbchWsUrl string) *AppConfig {
	if home == "" {
		home = os.ExpandEnv("$HOME/.follower")
	}

	if sbchRpcUrl == "" {
		sbchRpcUrl = "http://0.0.0.0:8545"
	}

	if sbchWsUrl == "" {
		sbchWsUrl = "ws://0.0.0.0:8546"
	}

	return &AppConfig{
		RootPath:                home,
		GenesisFilePath:         filepath.Join(home, "config", "genesis.json"),
		AppDataPath:             filepath.Join(home, "data", AppDataPath),
		ModbDataPath:            filepath.Join(home, "data", ModbDataPath),
		RpcEthGetLogsMaxResults: DefaultRpcEthGetLogsMaxResults,
		NumKeptBlocks:           DefaultNumKeptBlocks,
		NumKeptBlocksInMoDB:     DefaultNumKeptBlocksInMoDB,
		TrunkCacheSize:          DefaultTrunkCacheSize,
		PruneEveryN:             DefaultPruneEveryN,
		SmartBchRPCUrl:          sbchRpcUrl,
		SmartBchWsUrl:           sbchWsUrl,
		LogLevel:                "info",
	}
}
