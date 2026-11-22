package strategy

import (
	"errors"
	"strings"
	"testing"
)

func TestNativeProfileRoundTrip(t *testing.T) {
	raw := "\t--name=tls-first  --filter-tcp=853,443\r\n--filter-l7=tls --payload=tls_client_hello\n--lua-desync=multisplit:pos=9\n--new=passthrough\n--filter-tcp=8080\n"
	value, err := ParseNative("fixture", raw)
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := FormatNative(value)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := ParseNative("fixture", formatted)
	if err != nil || reloaded.Revision != value.Revision || reloaded.Profiles[0].ID != "tls-first" || reloaded.Profiles[1].ID != "passthrough" {
		t.Fatalf("native profile semantics changed after a visual round trip: %v", err)
	}
}

func TestNativeProfileRejectsExecutorAndAuthorityOptions(t *testing.T) {
	for _, raw := range []string{
		"--filter-tcp=443 --lua-init=@/tmp/local.lua",
		"--filter-tcp=443 --filter-mark=123",
		"--filter-tcp=443 --qnum=11",
		"--filter-tcp=443 --hostlist=/etc/hosts",
		"--filter-udp=443",
		"--filter-tcp=443 --filter-tcp=80",
		"--filter-tcp=443 --filter-l7=tls",
		"--filter-tcp=443 --filter-l7=http --payload=tls_client_hello --lua-desync=multisplit:pos=9",
		"--filter-tcp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=multisplit:pos=9:extra=1",
		"--filter-tcp=443 --new",
		strings.Repeat(" ", MaxNativeBytes+1),
	} {
		if _, err := ParseNative("fixture", raw); !errors.Is(err, ErrInvalidStrategy) {
			t.Fatalf("accepted unqualified native profile %q: %v", raw, err)
		}
	}
}
