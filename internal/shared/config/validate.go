package config

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

// Validate validates the configuration struct.
func Validate(cfg *Config) error {
	v := validator.New()
	if err := v.Struct(cfg); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	return nil
}
