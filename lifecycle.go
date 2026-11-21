package zapret

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-host/componenthost"
)

// ErrPacketPathUnavailable is returned by future activation adapters when
// ComponentHost has not been composed with a qualified packet-path adapter.
var ErrPacketPathUnavailable = errors.New("Zapret packet path unavailable in this build")

type Component struct{}

var _ componenthost.Lifecycle = Component{}

func (Component) Start(context.Context, componenthost.Ports) error { return nil }
func (Component) Stop(context.Context) error                       { return nil }
