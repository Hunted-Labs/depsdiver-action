package config

import (
	"encoding/yaml"
	"flag"
	"os"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type DatabaseConfig struct {
	DSN string `yaml:"dsn"`
}

type RedisConfig struct {
	Addr string `yaml:"addr"`
}

func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func LoadFromFlags() *Config {
	host := flag.String("host", "localhost", "Server host")
	port := flag.Int("port", 8080, "Server port")
	dsn := flag.String("dsn", "", "Database DSN")
	flag.Parse()

	return &Config{
		Server: ServerConfig{
			Host: *host,
			Port: *port,
		},
		Database: DatabaseConfig{
			DSN: *dsn,
		},
	}
}

