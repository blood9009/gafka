package disk

import (
	"errors"
	"time"
)

type Config struct {
	Dir           string // TODO []string to load balance disk IO
	PurgeInterval time.Duration
}

func DefaultConfig() *Config {
	return &Config{
		PurgeInterval: time.Minute * 10,
	}
}

func (this *Config) Validate() error {
	if this.Dir == "" {
		return errors.New("Dir must be specified")
	}

	return nil
}
