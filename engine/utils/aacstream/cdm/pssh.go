package wv

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func GetCertData(client *http.Client, licenseURL string) ([]byte, error) {
	response, err := client.Post(licenseURL, "application/x-www-form-urlencoded", bytes.NewReader([]byte{0x08, 0x04}))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("license server certificate request: HTTP %d", response.StatusCode)
	}
	// A service certificate is a few KB; an error page or runaway body must not be read whole.
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}
