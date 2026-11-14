package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Config is the one JSON file named on the command line.
// Paths are relative to the process working directory.
type Config struct {
	Listen            string         `json:"listen"`
	ImplicitTLSListen string         `json:"implicit_tls_listen"`
	CertFile          string         `json:"cert_file"`
	KeyFile           string         `json:"key_file"`
	ClientCAFile      string         `json:"client_ca_file,omitempty"`
	LogPath           string         `json:"log_path"`
	Personality       string         `json:"personality"`
	Dataset           string         `json:"dataset"`
	Control           string         `json:"control"`
	SmtpListen        smtpListenList `json:"smtp_listen,omitempty"`
	SmtpDSN           bool           `json:"smtp_dsn,omitempty"`
	SmtpTLSListen     smtpListenList `json:"smtp_tls_listen,omitempty"`
	SmtpAuthRequired  *bool          `json:"smtp_auth_required,omitempty"`
	SmtpSize          int            `json:"smtp_size,omitempty"`
	JmapListen         string         `json:"jmap_listen"`
	JmapTLSListen      string         `json:"jmap_tls_listen"`
	SieveListen        string         `json:"sieve_listen"`
	OauthListen        string         `json:"oauth_listen"`
	OauthTLSListen     string         `json:"oauth_tls_listen"`
	OauthRotateRefresh *bool          `json:"oauth_rotate_refresh,omitempty"`
	OauthAccessSeconds int            `json:"oauth_access_seconds,omitempty"`
	OauthRefreshSeconds int           `json:"oauth_refresh_seconds,omitempty"`
	OauthCodeSeconds   int            `json:"oauth_code_seconds,omitempty"`
	OauthClients       []oauthClientJSON `json:"oauth_clients,omitempty"`
}

type oauthClientJSON struct {
	ID        string   `json:"id"`
	Secret    string   `json:"secret,omitempty"`
	Redirects []string `json:"redirect_uris"`
}

// smtpListener is one SMTP bind. Addr is host:port.
type smtpListener struct {
	Addr string
	DSN  bool
}

// smtpListenList is a string, or an array of host:port strings and objects.
type smtpListenList []smtpListener

func (l smtpListenList) MarshalJSON() ([]byte, error) {
	if len(l) == 0 {
		return []byte(`""`), nil
	}
	if len(l) == 1 && !l[0].DSN {
		return json.Marshal(l[0].Addr)
	}
	raw := make([]any, len(l))
	for i, item := range l {
		if item.DSN {
			raw[i] = struct {
				Addr string `json:"addr"`
				DSN  bool   `json:"dsn"`
			}{item.Addr, true}
		} else {
			raw[i] = item.Addr
		}
	}
	return json.Marshal(raw)
}

func (l *smtpListenList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*l = nil
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*l = nil
			return nil
		}
		*l = smtpListenList{{Addr: s}}
		return nil
	}
	if b[0] != '[' {
		return fmt.Errorf("smtp listen must be a string or an array")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	out := make(smtpListenList, 0, len(raw))
	for _, item := range raw {
		item = bytes.TrimSpace(item)
		if len(item) == 0 {
			return fmt.Errorf("smtp listen entry")
		}
		if item[0] == '"' {
			var s string
			if err := json.Unmarshal(item, &s); err != nil {
				return err
			}
			if s == "" {
				return fmt.Errorf("smtp listen entry")
			}
			out = append(out, smtpListener{Addr: s})
			continue
		}
		if item[0] != '{' {
			return fmt.Errorf("smtp listen entry")
		}
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.DisallowUnknownFields()
		var obj struct {
			Addr string `json:"addr"`
			DSN  *bool  `json:"dsn"`
		}
		if err := dec.Decode(&obj); err != nil {
			return err
		}
		if obj.Addr == "" {
			return fmt.Errorf("smtp listen entry")
		}
		ent := smtpListener{Addr: obj.Addr}
		if obj.DSN != nil {
			ent.DSN = *obj.DSN
		}
		out = append(out, ent)
	}
	*l = out
	return nil
}

func (c Config) smtpAuthRequired() bool {
	if c.SmtpAuthRequired == nil {
		return true
	}
	return *c.SmtpAuthRequired
}

func (c Config) smtpSizeLimit() int {
	if c.SmtpSize <= 0 {
		return smtpSizeDefault
	}
	return c.SmtpSize
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
	if cfg.OauthTLSListen != "" && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return Config{}, fmt.Errorf("config %s: OAuth TLS needs cert_file and key_file", path)
	}
	if cfg.OauthAccessSeconds < 0 || cfg.OauthRefreshSeconds < 0 || cfg.OauthCodeSeconds < 0 {
		return Config{}, fmt.Errorf("config %s: oauth lifetime must be positive", path)
	}
	if len(cfg.SmtpTLSListen) > 0 && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return Config{}, fmt.Errorf("config %s: SMTP TLS needs cert_file and key_file", path)
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return Config{}, fmt.Errorf("config %s: cert_file and key_file are set together", path)
	}
	if cfg.SmtpSize < 0 {
		return Config{}, fmt.Errorf("config %s: smtp_size must be positive", path)
	}
	if cfg.SmtpDSN {
		if len(cfg.SmtpListen) != 1 {
			return Config{}, fmt.Errorf("config %s: smtp_dsn needs one smtp_listen", path)
		}
		cfg.SmtpListen[0].DSN = true
	}
	return cfg, nil
}
