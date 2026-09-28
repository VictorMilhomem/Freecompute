package chainbridge

import (
	"fmt"
	"time"

	"github.com/VictorMilhomem/freecompute/freechains"
)

// Config configures the ChainBridge actor.
type Config struct {
	Alias            freechains.Alias
	SigningKey       freechains.PrivateKeyPath
	Peers            []string
	PollInterval     time.Duration
	SyncInterval     time.Duration
	MaxPostBytes     int
	SupportedVersion uint32
}

// Validate checks configuration values and applies defaults where appropriate.
func (c *Config) Validate() error {
	if err := c.Alias.Validate(); err != nil {
		return fmt.Errorf("%w: invalid alias: %v", freechains.ErrInvalidArgument, err)
	}

	if c.SigningKey != "" {
		if err := c.SigningKey.Validate(); err != nil {
			return fmt.Errorf("%w: invalid signing key: %v", freechains.ErrInvalidArgument, err)
		}
	}

	if c.PollInterval == 0 {
		c.PollInterval = 5 * time.Second
	} else if c.PollInterval < 0 {
		return fmt.Errorf("%w: poll interval cannot be negative", freechains.ErrInvalidArgument)
	}

	if c.SyncInterval == 0 {
		c.SyncInterval = 30 * time.Second
	} else if c.SyncInterval < 0 {
		return fmt.Errorf("%w: sync interval cannot be negative", freechains.ErrInvalidArgument)
	}

	if c.MaxPostBytes == 0 {
		c.MaxPostBytes = freechains.MaxPayloadSize
	} else if c.MaxPostBytes < 0 {
		return fmt.Errorf("%w: max post bytes cannot be negative", freechains.ErrInvalidArgument)
	} else if c.MaxPostBytes > freechains.MaxPayloadSize {
		return fmt.Errorf("%w: max post bytes %d exceeds limit %d", freechains.ErrPayloadTooLarge, c.MaxPostBytes, freechains.MaxPayloadSize)
	}

	if c.SupportedVersion == 0 {
		c.SupportedVersion = 1
	}

	return nil
}
