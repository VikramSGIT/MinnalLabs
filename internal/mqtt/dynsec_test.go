package mqtt

import (
	"errors"
	"testing"
)

func TestIgnoreMissingDynsecError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "not found", err: errors.New("deleteClient: client not found"), want: true},
		{name: "does not exist", err: errors.New("deleteRole: role does not exist"), want: true},
		{name: "other error", err: errors.New("timed out waiting for dynsec response"), want: false},
		{name: "nil error", err: nil, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ignoreMissingDynsecError(tc.err); got != tc.want {
				t.Fatalf("ignoreMissingDynsecError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
