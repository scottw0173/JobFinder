package main

import (
	"testing"
	"time"
)

func TestTokenBudget(t *testing.T) {
	tests := []struct {
		tpm  int
		want float64
	}{
		{200000, 150000},
		{1000, 750},
	}
	for _, tt := range tests {
		if got := tokenBudget(tt.tpm); got != tt.want {
			t.Errorf("tokenBudget(%d) = %v, want %v", tt.tpm, got, tt.want)
		}
	}
}

func TestRequestInterval(t *testing.T) {
	tests := []struct {
		rpm  int
		want time.Duration
	}{
		{12, 7 * time.Second},  // 60/(0.75*12) = 6.67 -> ceil -> 7
		{100, 1 * time.Second}, // 60/(0.75*100) = 0.8 -> ceil -> 1
		{45, 2 * time.Second},  // 60/(0.75*45) = 1.78 -> ceil -> 2 (floor would wrongly give 1)
	}
	for _, tt := range tests {
		if got := requestInterval(tt.rpm); got != tt.want {
			t.Errorf("requestInterval(%d) = %v, want %v", tt.rpm, got, tt.want)
		}
	}
}

func TestNewModelThrottle(t *testing.T) {
	throttle, ticker := newModelThrottle(ModelConfig{TPM: 1000, RPM: 100})
	defer ticker.Stop()

	if throttle.budget != 750 {
		t.Errorf("throttle.budget = %v, want 750", throttle.budget)
	}
	if ticker == nil {
		t.Fatal("expected a non-nil ticker")
	}
}
