package docs

import (
	_ "embed"
)

//go:embed swagger.json
var SwaggerJSON []byte

//go:embed swagger.yaml
var SwaggerYAML []byte
