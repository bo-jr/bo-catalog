package migrate

import (
	"regexp"
	"testing"
)

// Applying against a real Postgres is exercised in the sandbox (see the
// bo-platform Phase 2 evidence); this guards what can break without one.
func TestVersionsAreOrderedAndWellNamed(t *testing.T) {
	vs, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) < 2 || vs[0] != "0001_items" || vs[1] != "0002_seed" {
		t.Fatalf("unexpected migrations: %v", vs)
	}
	name := regexp.MustCompile(`^\d{4}_[a-z0-9_]+$`)
	seen := map[string]bool{}
	for _, v := range vs {
		if !name.MatchString(v) {
			t.Errorf("%q: want NNNN_lower_snake", v)
		}
		if seen[v[:4]] {
			t.Errorf("%q: duplicate sequence number", v)
		}
		seen[v[:4]] = true
	}
}
