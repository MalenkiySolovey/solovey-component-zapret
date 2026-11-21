package zapret

import (
	"context"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/componenthost"
)

func TestComponentHostLifecycleDoesNotCreatePacketWork(t *testing.T) {
	component := Component{}
	if err := component.Start(context.Background(), componenthost.Ports{}); err != nil {
		t.Fatal(err)
	}
	if err := component.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
