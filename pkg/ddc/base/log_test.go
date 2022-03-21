package base

import (
	"context"
	"testing"

	"github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	fluiderrs "github.com/fluid-cloudnative/fluid/pkg/errors"
)

func TestLoggingErrorExceptConflict(t *testing.T) {

	engine := NewTemplateEngine(nil, "id", runtime.ReconcileRequestContext{
		Context: context.Background(),
		NamespacedName: types.NamespacedName{
			Namespace: "test",
			Name:      "test",
		},
		Log: fake.NullLogger(),
	})

	err := engine.loggingErrorExceptConflict(fluiderrs.NewDeprecated(schema.GroupResource{Group: "", Resource: "test"}, types.NamespacedName{}), "test")
	if !fluiderrs.IsDeprecated(err) {
		t.Errorf("Failed to check deprecated error %v", err)
	}
}
