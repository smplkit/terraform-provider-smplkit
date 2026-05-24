//go:build tools
// +build tools

// Package tools tracks build-time tools as Go dependencies so `go install`
// in CI picks up exactly the version recorded in go.sum. Only used to pin
// `tfplugindocs`, which generates the in-repo Terraform Registry docs.
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
