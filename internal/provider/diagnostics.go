package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// errorKind is a coarse classification used to decide how a Terraform
// operation should react to an SDK error — e.g. whether it should treat
// the result as "resource gone, remove from state" vs surfacing a hard
// failure.
type errorKind int

const (
	errKindOther errorKind = iota
	errKindNotFound
	errKindConflict
	errKindValidation
	errKindPaymentRequired
)

// classifySDKError walks the SDK's typed-error chain and reports both
// the high-level kind and the base smplkit.Error (when one is present)
// so callers can pull JSON:API details out of it.
func classifySDKError(err error) (errorKind, *smplkit.Error) {
	if err == nil {
		return errKindOther, nil
	}
	var (
		nf *smplkit.NotFoundError
		cf *smplkit.ConflictError
		ve *smplkit.ValidationError
		pr *smplkit.PaymentRequiredError
	)
	switch {
	case errors.As(err, &nf):
		return errKindNotFound, &nf.Base
	case errors.As(err, &cf):
		return errKindConflict, &cf.Base
	case errors.As(err, &ve):
		return errKindValidation, &ve.Base
	case errors.As(err, &pr):
		return errKindPaymentRequired, &pr.Base
	}
	var base *smplkit.Error
	if errors.As(err, &base) {
		return errKindOther, base
	}
	return errKindOther, nil
}

// isNotFound reports whether the SDK signaled a 404. Used by Read
// implementations to remove resources from Terraform state cleanly when
// they've been deleted out-of-band.
func isNotFound(err error) bool {
	kind, _ := classifySDKError(err)
	return kind == errKindNotFound
}

// addSDKErrorDiagnostic appends a Terraform diagnostic that mirrors the
// SDK error as closely as possible. The first JSON:API error detail
// becomes the diagnostic summary; remaining details and the message body
// are folded into the detail block. 402 (plan limit) gets a tailored
// summary so customers see *why* the apply failed rather than a generic
// "operation failed" line.
func addSDKErrorDiagnostic(diags *diag.Diagnostics, summary string, err error) {
	kind, base := classifySDKError(err)
	if base == nil {
		diags.AddError(summary, err.Error())
		return
	}

	switch kind {
	case errKindPaymentRequired:
		diags.AddError(
			"Plan limit reached — upgrade required",
			fmt.Sprintf("%s\n\n%s\n\nReview the smplkit pricing page for upgrade options, or remove resources to free a slot. Server response:\n%s",
				summary, formatDetail(base), formatErrors(base.Errors)),
		)
	case errKindValidation:
		diags.AddError(
			"Validation error from smplkit API",
			fmt.Sprintf("%s\n\n%s\n\n%s", summary, formatDetail(base), formatErrors(base.Errors)),
		)
	case errKindConflict:
		diags.AddError(
			"Resource conflict",
			fmt.Sprintf("%s\n\n%s\n\nA resource with this id may already exist. To adopt an existing resource, use `terraform import <type>.<name> <id>`. Server response:\n%s",
				summary, formatDetail(base), formatErrors(base.Errors)),
		)
	case errKindNotFound:
		diags.AddError(summary, fmt.Sprintf("%s\n\n%s", formatDetail(base), formatErrors(base.Errors)))
	default:
		diags.AddError(summary, fmt.Sprintf("%s\n\n%s", formatDetail(base), formatErrors(base.Errors)))
	}
}

func formatDetail(base *smplkit.Error) string {
	parts := []string{}
	if base.Message != "" {
		parts = append(parts, base.Message)
	}
	if base.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d %s", base.StatusCode, http.StatusText(base.StatusCode)))
	}
	return strings.Join(parts, " — ")
}

func formatErrors(details []smplkit.ApiErrorDetail) string {
	if len(details) == 0 {
		return ""
	}
	lines := make([]string, 0, len(details))
	for _, d := range details {
		var b strings.Builder
		b.WriteString("- ")
		if d.Code != "" {
			fmt.Fprintf(&b, "[%s] ", d.Code)
		}
		if d.Title != "" {
			b.WriteString(d.Title)
		}
		if d.Detail != "" {
			if d.Title != "" {
				b.WriteString(": ")
			}
			b.WriteString(d.Detail)
		}
		if d.Source.Pointer != "" {
			fmt.Fprintf(&b, " (at %s)", d.Source.Pointer)
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}
