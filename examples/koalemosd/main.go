package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/lytics/metafora"
	"github.com/lytics/metafora/examples/koalemos"
	"github.com/lytics/metafora/m_etcd"
)

func main() {
	mlvl := metafora.LogLevelInfo
	hostname, _ := os.Hostname()

	peers := flag.String("etcd", "http://127.0.0.1:2379", "comma delimited etcd peer list")
	namespace := flag.String("namespace", "koalemos", "metafora namespace")
	name := flag.String("name", hostname, "node name or empty for automatic")
	loglvl := flag.String("log", mlvl.String(), "set log level: [debug], info, warn, error")
	flag.Parse()

	hosts := strings.Split(*peers, ",")
	etcdc := etcd.NewClient(hosts)

	switch strings.ToLower(*loglvl) {
	case "debug":
		mlvl = metafora.LogLevelDebug
	case "info":
		mlvl = metafora.LogLevelInfo
	case "warn":
		mlvl = metafora.LogLevelWarn
	case "error":
		mlvl = metafora.LogLevelError
	default:
		metafora.Warnf("Invalid log level %q - using %s", *loglvl, mlvl)
	}
	metafora.SetLogLevel(mlvl)

	conf := m_etcd.NewConfig(*name, *namespace, hosts)

	// Replace NewTask func with one that returns a *koalemos.Task
	conf.NewTaskFunc = func(id, value string) metafora.Task {
		t := koalemos.NewTask(id)
		if value == "" {
			return t
		}
		if err := json.Unmarshal([]byte(value), t); err != nil {
			metafora.Errorf("Unable to unmarshal task %s: %v", t.ID(), err)
			return nil
		}
		return t
	}

	hfunc := makeHandlerFunc(etcdc)
	ec, err := m_etcd.NewEtcdCoordinator(conf)
	if err != nil {
		metafora.Errorf("Error creating etcd coordinator: %v", err)
	}

	bal := m_etcd.NewFairBalancer(conf)
	c, err := metafora.NewConsumer(ec, hfunc, bal, 10*time.Minute)
	if err != nil {
		metafora.Errorf("Error creating consumer: %v", err)
		os.Exit(2)
	}
	metafora.Infof(
		"Starting koalsmosd with etcd=%s; namespace=%s; name=%s; loglvl=%s",
		*peers, conf.Namespace, conf.Name, mlvl)
	consumerRunning := make(chan struct{})
	go func() {
		defer close(consumerRunning)
		c.Run()
	}()

	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, os.Kill, syscall.SIGTERM)
	select {
	case s := <-sigC:
		metafora.Infof("Received signal %s, shutting down", s)
	case <-consumerRunning:
		metafora.Warn("Consumer exited. Shutting down.")
	}
	c.Shutdown()
	metafora.Info("Shutdown")
}
