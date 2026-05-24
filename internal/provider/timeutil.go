package provider

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// timePointerToString folds a `*time.Time` returned by the SDK into a
// Terraform String — null when the pointer is nil, RFC3339 otherwise.
func timePointerToString(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}
