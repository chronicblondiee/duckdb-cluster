package cluster

import (
	"encoding/json"
	"os"
)

type Config struct {
	DataDir    string `json:"data_dir"`
	NumShards  int    `json:"num_shards"`
	ListenAddr string `json:"listen_addr"`
}

func DefaultConfig() *Config {
	return &Config{
		DataDir:    "./data",
		NumShards:  3,
		ListenAddr: ":8080",
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
