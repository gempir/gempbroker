package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/op/go-logging"
)

var (
	cfg config
	// Log logger from go-logging
	Log logging.Logger

	bots = make(map[string]*bot)

	// sync all bots joins since its ip based and not account based
	joinTicker = time.NewTicker(300 * time.Millisecond)
)

type config struct {
	BrokerPort string `json:"broker_port"`
	BrokerPass string `json:"broker_pass"`
}

func main() {

	Log = initLogger()
	cfg, err := readConfig("config.json")
	if err != nil {
		Log.Fatal(err)
	}

	Log.Info("starting up on port", cfg.BrokerPort)
	server := new(Server)
	port, err := strconv.Atoi(cfg.BrokerPort)
	if err != nil {
		panic("can't parse broker port")
	}
	go func() {
		Log.Error(http.ListenAndServe("localhost:9001", nil))
	}()
	server.startServer(port)
}

func initLogger() logging.Logger {
	var logger *logging.Logger
	logger = logging.MustGetLogger("relaybroker")
	backend1 := logging.NewLogBackend(os.Stdout, "", 0)
	backend2 := logging.NewLogBackend(os.Stdout, "", 0)
	format := logging.MustStringFormatter(
		`%{color}%{time:2006-01-02 15:04:05.000} %{shortfile:-15s} %{level:.4s}%{color:reset} %{message}`,
	)
	backend2Formatter := logging.NewBackendFormatter(backend2, format)
	backend1Leveled := logging.AddModuleLevel(backend1)
	backend1Leveled.SetLevel(logging.ERROR, "")
	logging.SetBackend(backend1Leveled, backend2Formatter)
	return *logger
}

func readConfig(path string) (config, error) {
	var cfg config
	file, err := ioutil.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	return unmarshalConfig(file)
}

func unmarshalConfig(file []byte) (config, error) {
	var cfg config
	err := json.Unmarshal(file, &cfg)
	if err != nil {
		return cfg, err
	}
	return cfg, nil
}
