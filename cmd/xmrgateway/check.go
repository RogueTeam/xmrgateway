package main

import "fmt"

func checkCmd(args []string) error {
	fs, home := newFlags("check")
	configPath := fs.String("config", "config.json", "config file")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	if _, err := loadConfig(*configPath); err != nil {
		return err
	}
	fmt.Println(*configPath, "is valid")
	return nil
}
