package wv

// Public source snapshot:
// No embedded Widevine device identity is distributed.
// A valid externally provisioned identity is required.

var DefaultPrivateKey string
var DefaultClientID []byte

func loadEmbedded() {
    DefaultPrivateKey = ""
    DefaultClientID = nil
}
