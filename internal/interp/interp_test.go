package interp_test

import (
	"testing"

	"github.com/YewFence/stacklint/internal/interp"
)

func TestNames(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		want       []string
	}{
		{"plain", "${BIND_ADDRESS}", []string{"BIND_ADDRESS"}},
		{"with default", "${BIND_ADDRESS:-127.0.0.1}", []string{"BIND_ADDRESS"}},
		{"unbraced", "$BIND_ADDRESS", []string{"BIND_ADDRESS"}},
		{"literal", "127.0.0.1", nil},
		{"escaped", "$${BIND_ADDRESS}", nil},
		{"nested", "${URL:-https://${DOMAIN}}", []string{"DOMAIN", "URL"}},
		{"two variables", "${HOST}:${PORT}", []string{"HOST", "PORT"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := interp.Names(test.expression)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("Names(%q) = %v, want %v", test.expression, got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("Names(%q) = %v, want %v", test.expression, got, test.want)
				}
			}
		})
	}
}

func TestVariablesBehavior(t *testing.T) {
	variables, err := interp.Variables("${VALUE:-fallback}")
	if err != nil {
		t.Fatal(err)
	}
	behavior := variables["VALUE"]
	if behavior == nil || behavior.Default == nil || *behavior.Default != "fallback" {
		t.Fatalf("expected soft default fallback, got %+v", behavior)
	}

	variables, err = interp.Variables("${VALUE:?required}")
	if err != nil {
		t.Fatal(err)
	}
	behavior = variables["VALUE"]
	if behavior == nil || !behavior.Required || !behavior.Nonempty {
		t.Fatalf("expected required nonempty, got %+v", behavior)
	}

	variables, err = interp.Variables(`{"a": "${OTHER:-x}", "password": "${TOKEN:?required}"}`)
	if err != nil {
		t.Fatal(err)
	}
	behavior = variables["TOKEN"]
	if behavior == nil || !behavior.Required || !behavior.Nonempty {
		t.Fatalf("expected required nonempty alongside other variables, got %+v", behavior)
	}
	behavior = variables["OTHER"]
	if behavior == nil || behavior.Default == nil || *behavior.Default != "x" {
		t.Fatalf("expected soft default x alongside other variables, got %+v", behavior)
	}
}

func TestInvalidExpression(t *testing.T) {
	if _, err := interp.Variables("${UNCLOSED"); err == nil {
		t.Fatal("expected error for unclosed expression")
	}
}
