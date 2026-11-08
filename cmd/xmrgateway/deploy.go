package main

import (
	"fmt"

	"github.com/RogueTeam/xmrgateway/web"
)

func deployCmd(args []string) error {
	fs, home := newFlags("deploy")
	dir := fs.String("dir", "static", "directory to write the assets to, same as static_dir in the config")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	if err := web.Deploy(*dir); err != nil {
		return err
	}
	fmt.Println("static assets written to", *dir)
	return nil
}
