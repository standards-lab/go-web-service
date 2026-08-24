package main

import "embed"

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed seeds/*.json
var seeds embed.FS
