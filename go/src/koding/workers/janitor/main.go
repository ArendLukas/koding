package main

import (
	"fmt"
	"koding/artifact"
	"koding/db/mongodb/modelhelper"
	"log"
	"net"
	"net/http"
	"socialapi/config"
	"time"

	kiteConfig "github.com/koding/kite/config"
	"github.com/koding/logging"
	"github.com/koding/runner"
	"github.com/robfig/cron"

	"github.com/koding/kite"
)

const (
	WorkerName    = "janitor"
	WorkerVersion = "0.0.1"

	// DailyAtEightAM specifies interval; cron runs at utc, 3pm UTC is 8am PST
	// with daylight savings time
	DailyAtEightAM = "0 0 3 * * *"
)

var (
	Log        logging.Logger
	KiteClient *kite.Client
)

func main() {
	var err error

	r := initializeRunner()

	conf := config.MustRead(r.Conf.Path)
	port := conf.Janitor.Port
	konf := conf.Kloud

	kloudSecretKey := conf.Janitor.SecretKey

	go r.Listen()

	KiteClient, err = initializeKiteClient(r, kloudSecretKey, konf.Address)
	if err != nil {
		Log.Fatal("Error initializing kite: %s", err.Error())
	}

	// warnings contains list of warnings to be iterated upon in a certain
	// interval.
	warnings := []*Warning{
		VMDeletionWarning1, VMDeletionWarning2, DeleteInactiveUserVM, DeleteBlockedUserVM,
	}

	c := cron.New()
	c.AddFunc(DailyAtEightAM, func() {
		for _, w := range warnings {

			// clone warning so local changes don't affect next run
			warning := *w

			result := warning.Run()
			Log.Info(result.String())
		}
	})

	c.Start()

	mux := http.NewServeMux()
	mux.HandleFunc("/version", artifact.VersionHandler())
	mux.HandleFunc("/healthCheck", artifact.HealthCheckHandler(WorkerName))

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		Log.Fatal("Error opening tcp connection: %s", err.Error())
	}

	Log.Info("Listening on port: %s", port)

	r.ShutdownHandler = func() {
		listener.Close()
		KiteClient.Close()
		modelhelper.Close()
	}

	if err := http.Serve(listener, mux); err != nil {
		Log.Fatal("Error starting http server: %s", err.Error())
	}
}

func initializeRunner() *runner.Runner {
	r := runner.New(WorkerName)
	if err := r.Init(); err != nil {
		log.Fatal("Error starting runner: %s", err.Error())
	}

	appConfig := config.MustRead(r.Conf.Path)
	modelhelper.Initialize(appConfig.Mongo)

	Log = r.Log

	return r
}

func initializeKiteClient(r *runner.Runner, kloudKey, kloudAddr string) (*kite.Client, error) {
	config, err := kiteConfig.Get()
	if err != nil {
		return nil, err
	}

	// set skeleton config
	r.Kite.Config = config

	// create a new connection to the cloud
	kiteClient := r.Kite.NewClient(kloudAddr)
	kiteClient.Auth = &kite.Auth{Type: WorkerName, Key: kloudKey}
	kiteClient.Reconnect = true

	// dial the kloud address
	if err := kiteClient.DialTimeout(time.Second * 10); err != nil {
		return nil, fmt.Errorf("%s. Is kloud running?", err.Error())
	}

	Log.Debug("Connected to klient: %s", kloudAddr)

	return kiteClient, nil
}
