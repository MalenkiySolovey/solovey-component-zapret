package strategy

import "strings"

// Names from nfq2/protocol.c in the pinned engine. Pass profiles do not need
// the stricter payload/action pairing used by the transforming actions.
const nativeL7Names = ",all,unknown,known,http,tls,dtls,quic,wireguard,dht,discord,stun,xmpp,dns,mtproto,bt,utp_bt,"
const nativePayloadNames = ",all,unknown,empty,known,ipv4,ipv6,icmp,http_req,http_reply,tls_client_hello,tls_server_hello," +
	"dtls_client_hello,dtls_server_hello,quic_initial,wireguard_initiation,wireguard_response,wireguard_cookie," +
	"wireguard_keepalive,wireguard_data,dht,discord_ip_discovery,stun,xmpp_stream,xmpp_starttls,xmpp_proceed," +
	"xmpp_features,dns_query,dns_response,mtproto_initial,bt_handshake,utp_bt_handshake,"

func validNativeSelector(value, names string) bool {
	for name := range strings.SplitSeq(value, ",") {
		if name == "" || !strings.Contains(names, ","+name+",") {
			return false
		}
	}
	return true
}

func passiveMayShadowQUIC(filter string) bool {
	if filter == "" {
		return true
	}
	for protocol := range strings.SplitSeq(filter, ",") {
		switch protocol {
		case "all", "known", "quic", "unknown":
			// Unknown may win before recognition. Don't claim DNS coverage.
			return true
		}
	}
	return false
}
