package main

import "testing"

func TestDeliveryWindow(t *testing.T) {
	tests := []struct {
		stage   string
		seconds int
		allowed bool
	}{
		{"intake", 900, true},
		{"signed_document", 3600, true},
		{"deadline_followup", 300, true},
		{"closed", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.stage, func(t *testing.T) {
			seconds, allowed := deliveryWindow(tt.stage)
			if seconds != tt.seconds || allowed != tt.allowed {
				t.Fatalf("got (%d, %t), want (%d, %t)", seconds, allowed, tt.seconds, tt.allowed)
			}
		})
	}
}
