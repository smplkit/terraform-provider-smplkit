package provider

import (
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	smplkit "github.com/smplkit/go-sdk/v3"
)

func TestClassifySDKError_NotFound(t *testing.T) {
	err := &smplkit.NotFoundError{Base: smplkit.Error{Message: "gone", StatusCode: 404}}
	kind, base := classifySDKError(err)
	if kind != errKindNotFound {
		t.Errorf("kind: got %d want %d", kind, errKindNotFound)
	}
	if base == nil || base.StatusCode != 404 {
		t.Errorf("base: got %+v", base)
	}
}

func TestClassifySDKError_PaymentRequired(t *testing.T) {
	err := &smplkit.PaymentRequiredError{Base: smplkit.Error{
		Message:    "plan limit",
		StatusCode: 402,
		Errors: []smplkit.ApiErrorDetail{{
			Code:   "platform_managed_environments_limit",
			Title:  "Plan limit reached",
			Detail: "Upgrade to add more environments.",
		}},
	}}
	kind, base := classifySDKError(err)
	if kind != errKindPaymentRequired {
		t.Errorf("kind: got %d want %d", kind, errKindPaymentRequired)
	}
	if base == nil || base.StatusCode != 402 {
		t.Errorf("base: got %+v", base)
	}
}

func TestIsNotFound(t *testing.T) {
	if !isNotFound(&smplkit.NotFoundError{Base: smplkit.Error{StatusCode: 404}}) {
		t.Error("isNotFound should detect 404")
	}
	if isNotFound(errors.New("random")) {
		t.Error("isNotFound on stdlib error should be false")
	}
}

func TestAddSDKErrorDiagnostic_NoBase(t *testing.T) {
	var diags diag.Diagnostics
	addSDKErrorDiagnostic(&diags, "doing thing", errors.New("network down"))
	if !diags.HasError() {
		t.Fatal("expected diagnostic")
	}
	d := diags[0]
	if !strings.Contains(d.Detail(), "network down") {
		t.Errorf("expected detail to mention network down, got %q", d.Detail())
	}
}

func TestAddSDKErrorDiagnostic_402(t *testing.T) {
	var diags diag.Diagnostics
	err := &smplkit.PaymentRequiredError{Base: smplkit.Error{
		Message:    "plan limit reached",
		StatusCode: 402,
		Errors: []smplkit.ApiErrorDetail{{
			Code:   "platform_managed_environments_limit",
			Title:  "Plan limit reached",
			Detail: "Upgrade to add more environments.",
		}},
	}}
	addSDKErrorDiagnostic(&diags, "creating environment", err)
	if !diags.HasError() {
		t.Fatal("expected diagnostic")
	}
	d := diags[0]
	if !strings.Contains(d.Summary(), "Plan limit reached") {
		t.Errorf("summary should mention plan limit: %q", d.Summary())
	}
	if !strings.Contains(d.Detail(), "platform_managed_environments_limit") {
		t.Errorf("detail should include machine-readable code: %q", d.Detail())
	}
	if !strings.Contains(d.Detail(), "Upgrade") {
		t.Errorf("detail should include upgrade direction: %q", d.Detail())
	}
}
