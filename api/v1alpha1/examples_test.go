package v1alpha1_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/janpuc/dispatch/api/v1alpha1"
)

// TestExamplesMatchTypes strictly decodes every manifest under examples/ into
// the typed API, so the published examples can never drift from the schema.
func TestExamplesMatchTypes(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading examples dir: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}

		var head struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		}
		if err := yaml.Unmarshal(raw, &head); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if head.APIVersion != v1alpha1.GroupVersion.String() {
			t.Errorf("%s: apiVersion = %q, want %q", entry.Name(), head.APIVersion, v1alpha1.GroupVersion.String())
			continue
		}

		var target any
		switch head.Kind {
		case "Agent":
			target = &v1alpha1.Agent{}
		case "Trigger":
			target = &v1alpha1.Trigger{}
		case "Session":
			target = &v1alpha1.Session{}
		case "Workspace":
			target = &v1alpha1.Workspace{}
		default:
			t.Errorf("%s: unknown kind %q", entry.Name(), head.Kind)
			continue
		}
		if err := yaml.UnmarshalStrict(raw, target); err != nil {
			t.Errorf("%s: does not decode strictly into %s: %v", entry.Name(), head.Kind, err)
		}
		checked++
	}
	if checked < 7 {
		t.Errorf("checked %d manifests, expected at least 7", checked)
	}
}
