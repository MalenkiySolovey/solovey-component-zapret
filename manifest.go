package zapret

import (
	_ "embed"

	"github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
)

//go:embed component.json
var componentJSON []byte

// Manifest returns a fresh copy of the source-owned descriptor.
func Manifest() manifest.Manifest { return manifest.MustFromJSON(componentJSON) }
