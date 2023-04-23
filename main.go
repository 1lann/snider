package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/BurntSushi/toml"
	"go.uber.org/zap"
)

type Backend struct {
	Hostname string `toml:"hostname"`
	Protocol string `toml:"protocol"` // Go compatible protocol, i.e. "tcp" or "unix"
	Address  string `toml:"address"`
}

type Config struct {
	ListenAddr    string     `toml:"listen_address"`
	ListenProto   string     `toml:"listen_protocol"`
	AdvertiseName string     `toml:"advertise_name"`
	Backends      []*Backend `toml:"backend"`
}

func readConfig(pathToConfig string) (*Config, error) {
	f, err := os.Open(pathToConfig)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	var cfg Config
	_, err = toml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config.toml>\n", os.Args[0])
		os.Exit(1)
	}

	cfg, err := readConfig(os.Args[1])
	if err != nil {
		log.Printf("error reading config: %+v\n", err)
		os.Exit(2)
	}

	ln, err := net.Listen(cfg.ListenProto, cfg.ListenAddr)
	if err != nil {
		log.Printf("error listening: %+v\n", err)
		os.Exit(3)
	}

	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Printf("error creating logger: %+v\n", err)
		os.Exit(4)
	}

	s := &Server{
		cfg:    cfg,
		logger: logger.Sugar(),
	}

	logger.Sugar().Infof("Listening on %s://%s", cfg.ListenProto, cfg.ListenAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("error accepting connection: %+v\n", err)
			continue
		}

		go s.handleConnection(conn)
	}
}
