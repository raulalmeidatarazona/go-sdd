// Package openapi embeds the OpenAPI document generated from the .proto files.
package openapi

import _ "embed"

// Spec is the merged OpenAPI v2 document served at /openapi.json.
//
//go:embed oms.swagger.json
var Spec []byte
