package strategy

import "slices"

const (
	presetReferenceCommit  = "4934082a4069e2e66e35668cf6a03cf4c8de16d2"
	presetReferenceVersion = "22.0.0.0"
)

type PresetTemplate struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	Compatibility string `json:"compatibility"`
	Profiles      int    `json:"profiles"`
	Origin        string `json:"origin"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	SourcePath    string `json:"sourcePath"`
	SourceBlob    string `json:"sourceBlob"`
	License       string `json:"license"`
	source        string
}

const templateTLS = `--name=tls-split
--filter-tcp=443
--filter-l7=tls
--payload=tls_client_hello
--lua-desync=multisplit:pos=2
`

const templateWebQUIC = templateTLS + `--new=quic-initial
--filter-udp=443
--filter-l7=quic
--payload=quic_initial
--lua-desync=fake:blob=fake_default_quic:repeats=1
`

var presetTemplates = []PresetTemplate{
	referenceTemplate("tls-split", "TLS split", "One TCP profile for TLS ClientHello on port 443.", "tcp", "adapted", 1,
		"general ALT4.txt", "e27200c727eeafa8b9daf7b15b2ffdc8c5eefc8f", templateTLS),
	referenceTemplate("web-tcp-quic", "Web TCP and QUIC", "Ordered TLS and QUIC profiles on port 443, with the pinned built-in QUIC asset.", "web", "adapted", 2,
		"general ALT4.txt", "e27200c727eeafa8b9daf7b15b2ffdc8c5eefc8f", templateWebQUIC),
	referenceTemplate("windows-interception", "Windows interception example", "Review-only example of Windows interception and script-loading options that are not admitted on Solovey.", "reference", "preview-only", 1,
		"ALL TCP & UDP v1.txt", "d0f382e0b9fe2d13d51886507a044676f0ff4036", `# Illustrative excerpt, not a runnable Windows or Linux preset.
--wf-tcp-out=443
--lua-init=@lua/custom_funcs.lua
--name=windows-reference
--filter-tcp=443
--hostlist=lists/example.txt
`),
}

func referenceTemplate(id, name, description, category, compatibility string, profiles int, file, blob, source string) PresetTemplate {
	return PresetTemplate{ID: id, Name: name, Description: description, Category: category, Compatibility: compatibility, Profiles: profiles,
		Origin: "ZapretGUI", Commit: presetReferenceCommit, Version: presetReferenceVersion, License: "MIT (reference)",
		SourcePath: "src/presets/builtin/winws2/" + file, SourceBlob: blob, source: source}
}

func PresetTemplates() []PresetTemplate { return slices.Clone(presetTemplates) }

func findTemplate(id string) (PresetTemplate, bool) {
	for _, value := range presetTemplates {
		if value.ID == id {
			return value, true
		}
	}
	return PresetTemplate{}, false
}
