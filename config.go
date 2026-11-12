package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the one JSON file named on the command line.
// Paths are relative to the process working directory.
type Config struct {
	Listen            string `json:"listen"`
	ImplicitTLSListen string `json:"implicit_tls_listen"`
	CertFile          string `json:"cert_file"`
	KeyFile           string `json:"key_file"`
	ClientCAFile      string `json:"client_ca_file,omitempty"`
	LogPath           string `json:"log_path"`
	Personality       string `json:"personality"`
	Dataset           string `json:"dataset"`
	Control           string `json:"control"`
	SmtpListen        string `json:"smtp_listen"`
	JmapListen        string `json:"jmap_listen"`
	JmapTLSListen     string `json:"jmap_tls_listen"`
	SieveListen       string `json:"sieve_listen"`
}

func loadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if cfg.Listen == "" && cfg.ImplicitTLSListen == "" {
		return Config{}, fmt.Errorf("config %s: listen or implicit_tls_listen is required", path)
	}
	if cfg.Personality == "" {
		return Config{}, fmt.Errorf("config %s: personality is required", path)
	}
	if cfg.Dataset == "" {
		return Config{}, fmt.Errorf("config %s: dataset is required", path)
	}
	if cfg.ImplicitTLSListen != "" && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return Config{}, fmt.Errorf("config %s: implicit TLS needs cert_file and key_file", path)
	}
	if cfg.JmapTLSListen != "" && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return Config{}, fmt.Errorf("config %s: JMAP TLS needs cert_file and key_file", path)
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return Config{}, fmt.Errorf("config %s: cert_file and key_file are set together", path)
	}
	return cfg, nil
}
