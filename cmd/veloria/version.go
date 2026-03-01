package main

import (
	"fmt"

	"veloria/internal/config"
)

// VersionCmd prints version information.
type VersionCmd struct{}

func (c *VersionCmd) Run() error {
	fmt.Printf("veloria %s\n", config.Version)
	return nil
}
