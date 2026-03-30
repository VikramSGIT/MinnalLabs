package homejobs

import (
	"testing"
	"time"

	"github.com/iot-backend/internal/models"
)

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		name     string
		attempts int
		want     int
	}{
		{name: "normalizes zero attempt", attempts: 0, want: 1},
		{name: "scales linearly", attempts: 3, want: 3},
		{name: "caps large attempt", attempts: 9, want: 6},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := retryDelay(tc.attempts)
			want := time.Duration(tc.want) * WorkerInterval
			if got != want {
				t.Fatalf("retryDelay(%d) = %s, want %s", tc.attempts, got, want)
			}
		})
	}
}

func TestHomeAllowsDeviceProvisioning(t *testing.T) {
	cases := []struct {
		name string
		home models.Home
		want bool
	}{
		{
			name: "ready home is allowed",
			home: models.Home{MQTTProvisionState: models.HomeMQTTProvisionStateReady, MQTTUsername: "u", MQTTPassword: "p"},
			want: true,
		},
		{
			name: "pending home is allowed",
			home: models.Home{MQTTProvisionState: models.HomeMQTTProvisionStatePending, MQTTUsername: "u", MQTTPassword: "p"},
			want: true,
		},
		{
			name: "failed home is rejected",
			home: models.Home{MQTTProvisionState: models.HomeMQTTProvisionStateFailed, MQTTUsername: "u", MQTTPassword: "p"},
			want: false,
		},
		{
			name: "deleting home is rejected",
			home: models.Home{MQTTProvisionState: models.HomeMQTTProvisionStateDeleting, MQTTUsername: "u", MQTTPassword: "p"},
			want: false,
		},
		{
			name: "missing credentials implies rejection",
			home: models.Home{},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.home.AllowsDeviceProvisioning(); got != tc.want {
				t.Fatalf("AllowsDeviceProvisioning() = %v, want %v", got, tc.want)
			}
		})
	}
}
