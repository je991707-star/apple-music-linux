package wv

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/go-resty/resty/v2"

	wv "engine/utils/aacstream/cdm"
)

type Key struct {
	ReqCli *resty.Client

	BeforeRequest func(cl *resty.Client, ctx context.Context, url string, body []byte) (*resty.Response, error)

	AfterRequest func(*resty.Response) ([]byte, []byte, []byte, error) // Returns (license, iv, kid, error)
}

func (w *Key) CdmInit() {
	wv.InitConstants()
}

func (w *Key) GetKey(ctx context.Context, licenseServerURL string, PSSH string, headers map[string][]string) (string, []byte, []byte, []byte, error) {
	initData, err := base64.StdEncoding.DecodeString(PSSH)
	var keybt []byte
	var ivbt []byte
	var kidbt []byte
	if err != nil {
		slog.Error("pssh decode error", slog.Any("err", err))
		return "", keybt, ivbt, kidbt, err
	}
	cdm, err := wv.NewDefaultCDM(initData)
	if err != nil {
		slog.Error("cdm init error", slog.Any("err", err))
		return "", keybt, ivbt, kidbt, err
	}
	licenseRequest, err := cdm.GetLicenseRequest()
	if err != nil {
		slog.Error("license request error", slog.Any("err", err))
		return "", keybt, ivbt, kidbt, err
	}

	var response *resty.Response

	if w.BeforeRequest != nil {
		response, err = w.BeforeRequest(w.ReqCli, ctx, licenseServerURL, licenseRequest)
	} else {
		response, err = w.ReqCli.R().
			SetContext(ctx).
			SetBody(licenseRequest).
			Post(licenseServerURL)
	}

	if err != nil {
		slog.Error("license request error", slog.Any("err", err))
		return "", keybt, ivbt, kidbt, err
	}

	var licenseResponse []byte
	if w.AfterRequest != nil {
		licenseResponse, ivbt, kidbt, err = w.AfterRequest(response)
		if err != nil {
			return "", keybt, ivbt, kidbt, err
		}
	} else {
		licenseResponse = response.Body()
		ivbt = make([]byte, 16) // Default zero IV if no AfterRequest
		kidbt = make([]byte, 16) // Default zero KID if no AfterRequest
	}

	keys, err := cdm.GetLicenseKeys(licenseRequest, licenseResponse)
	if err != nil {
		return "", keybt, ivbt, kidbt, fmt.Errorf("parse license keys: %w", err)
	}
	command := ""

	for _, key := range keys {
		if key.Type == wv.License_KeyContainer_CONTENT {
			command += hex.EncodeToString(key.Value)
			keybt = key.Value
			// Extract KID from the Widevine Key struct as fallback
			// Primary KID extraction should come from libCoreFP.so CKC processing
			if len(key.ID) == 16 {
				kidbt = make([]byte, 16)
				copy(kidbt, key.ID)
				slog.Debug("Extracted KID from Widevine key (fallback)", slog.Any("kid", kidbt[:4]))
			}
		}
	}
	
	// Note: KID should ideally be extracted from libCoreFP.so X46O5IeS(op=3) processing
	// The Widevine KID is used as a fallback when libCoreFP is not available
	return command, keybt, ivbt, kidbt, nil
}
