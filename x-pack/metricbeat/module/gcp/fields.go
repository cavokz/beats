// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

// Code generated by beats/dev-tools/cmd/asset/asset.go - DO NOT EDIT.

package gcp

import (
	"github.com/elastic/beats/v7/libbeat/asset"
)

func init() {
	if err := asset.SetFields("metricbeat", "gcp", asset.ModuleFieldsPri, AssetGcp); err != nil {
		panic(err)
	}
}

// AssetGcp returns asset data.
// This is the base64 encoded zlib format compressed contents of module/gcp.
func AssetGcp() string {
	return "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"
}
