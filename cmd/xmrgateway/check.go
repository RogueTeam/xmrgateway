package main

import (
	"fmt"

	"github.com/RogueTeam/xmrgateway/internal/config"
)

func checkCmd(args []string) error {
	fs, home := newFlags("check")
	configPath := fs.String("config", "config.json", "config file")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%s:\n%w", *configPath, err)
	}
	fmt.Println(*configPath, "is valid")
	return nil
}
