//go:build u2_credential_fixture

package main

import "engine/core/drm"

func configurePrivateInputFixture(m *drm.DRMManager) {
	if m != nil {
		m.EnableSyntheticCredentialConsumer()
	}
}
