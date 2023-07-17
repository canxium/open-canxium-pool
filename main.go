//go:build go1.9
// +build go1.9

package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/yvasiyarov/gorelic"

	"github.com/yuriy0803/open-etc-pool-friends/api"
	"github.com/yuriy0803/open-etc-pool-friends/exchange"
	"github.com/yuriy0803/open-etc-pool-friends/payouts"
	"github.com/yuriy0803/open-etc-pool-friends/proxy"
	"github.com/yuriy0803/open-etc-pool-friends/storage"
)

var cfg proxy.Config
var backend *storage.RedisClient

func startProxy() {
	s := proxy.NewProxy(&cfg, backend)
	s.Start()
}

func startApi() {
	s := api.NewApiServer(&cfg.Api, backend)
	s.Start()
}

func startBlockUnlocker() {
	if cfg.Rpc != nil {
		cfg.BlockUnlocker.Daemon = *cfg.Rpc
	}

	u := payouts.NewBlockUnlocker(&cfg.BlockUnlocker, backend, cfg.Network)
	u.Start()
}

func startPayoutsProcessor() {
	if cfg.Rpc != nil {
		cfg.Payouts.Daemon = *cfg.Rpc
	}

	u := payouts.NewPayoutsProcessor(&cfg.Payouts, backend)
	u.Start()
}

func startExchangeProcessor() {
	u := exchange.StartExchangeProcessor(&cfg.Exchange, backend)
	u.Start()
}

func startNewrelic() {
	if cfg.NewrelicEnabled {
		nr := gorelic.NewAgent()
		nr.Verbose = cfg.NewrelicVerbose
		nr.NewrelicLicense = cfg.NewrelicKey
		nr.NewrelicName = cfg.NewrelicName
		nr.Run()
	}
}

func readConfig(cfg *proxy.Config) {
	configFileName := "config.json"
	if len(os.Args) > 1 {
		configFileName = os.Args[1]
	}
	configFileName, _ = filepath.Abs(configFileName)
	log.Printf("Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Fatal("File error: ", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err := jsonParser.Decode(&cfg); err != nil {
		log.Fatal("Config error: ", err.Error())
	}

	if difficulty, present := os.LookupEnv("MINING_DIFFICULTY"); present {
		if n, err := strconv.ParseInt(difficulty, 10, 64); err == nil {
			cfg.Difficulty = n

		}
	}

	if chainId, present := os.LookupEnv("MINING_CHAIN_ID"); present {
		if n, err := strconv.ParseInt(chainId, 10, 64); err == nil {
			cfg.ChainId = n

		}
	}

	if algorithm, present := os.LookupEnv("MINING_ALGORITHM"); present {
		if n, err := strconv.ParseInt(algorithm, 10, 64); err == nil {
			cfg.Algorithm = uint8(n)

		}
	}

	if coinbase, present := os.LookupEnv("MINING_COINBASE"); present {
		cfg.Coinbase = common.HexToAddress(coinbase)
	}

	if cfg.Coinbase == (common.Address{}) {
		log.Fatalf("Invalid mining coinbase address: %v", cfg.Coinbase)
	}

	if contract, present := os.LookupEnv("MINING_CONTRACT"); present {
		cfg.MiningContract = common.HexToAddress(contract)
	}

	if cfg.MiningContract == (common.Address{}) {
		log.Fatalf("Invalid mining contract address: %v", cfg.MiningContract)
	}

	if rpc, present := os.LookupEnv("CANXIUM_RPC"); present {
		cfg.Rpc = &rpc
	}

	log.Printf("Config loaded, chainId: %d, coinbase %v, algorithm: %v, difficulty: %d", cfg.ChainId, cfg.Coinbase, cfg.Algorithm, cfg.Difficulty)
}

func main() {
	readConfig(&cfg)
	rand.Seed(time.Now().UnixNano())

	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		log.Printf("Running with %v threads", cfg.Threads)
	}

	startNewrelic()

	backend = storage.NewRedisClient(&cfg.Redis, cfg.Coin, cfg.Pplns, cfg.CoinName)
	pong, err := backend.Check()
	if err != nil {
		log.Printf("Can't establish connection to backend: %v", err)
	} else {
		log.Printf("Backend check reply: %v", pong)
	}

	if cfg.Proxy.Enabled {
		go startProxy()
	}
	if cfg.Api.Enabled {
		go startApi()
	}
	if cfg.BlockUnlocker.Enabled {
		go startBlockUnlocker()
	}
	if cfg.Payouts.Enabled {
		go startPayoutsProcessor()
	}
	if cfg.Exchange.Enabled {
		go startExchangeProcessor()
	}
	quit := make(chan bool)
	<-quit
}
