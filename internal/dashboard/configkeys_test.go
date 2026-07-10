package dashboard

import (
	"testing"

	"github.com/maverickd650/kubepage-operator/internal/dashboard/widgetschema"
)

// staticInfoWidgetTypes are InfoWidget types that render statically
// (server.go) and never call Register, so they're invisible to
// RegisteredTypes() — mirrors headerTypeGreeting/headerTypeDatetime plus
// "logo" (api/v1alpha1/infowidget_types.go's Type enum).
var staticInfoWidgetTypes = []string{headerTypeGreeting, headerTypeDatetime, headerTypeLogo}

// TestConfigSchemasCoverRegisteredWidgets guards widgetschema.ConfigSchemas
// against the same kind of drift TestEveryRegisteredWidgetHasASample guards
// Sampler against: every widget type registered via Register (i.e. every
// real pollable widget type) must have a ConfigSchemas entry, or issue #104's
// config-key validation would silently skip a new widget instead of
// declaring its contract. Static InfoWidget types are checked separately
// since they never go through Register.
func TestConfigSchemasCoverRegisteredWidgets(t *testing.T) {
	for _, widgetType := range RegisteredTypes() {
		if _, ok := widgetschema.ConfigSchemas[widgetType]; !ok {
			t.Errorf("widget type %q is registered but has no widgetschema.ConfigSchemas entry; add one declaring its known config keys", widgetType)
		}
	}

	for _, widgetType := range staticInfoWidgetTypes {
		if _, ok := widgetschema.ConfigSchemas[widgetType]; !ok {
			t.Errorf("static InfoWidget type %q has no widgetschema.ConfigSchemas entry; add one declaring its known options keys", widgetType)
		}
	}
}
