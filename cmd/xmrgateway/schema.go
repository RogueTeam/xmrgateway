package main

import (
	"os"

	"github.com/RogueTeam/xmrgateway/internal/config"
)

func schemaCmd(args []string) error {
	fs, home := newFlags("schema")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	_, err := os.Stdout.Write(config.JSONSchema)
	return err
}
