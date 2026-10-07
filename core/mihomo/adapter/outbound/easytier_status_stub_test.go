//go:build no_easytier

package outbound

import (
	"context"
	"strings"
	"testing"
)

func TestEasyTierDisabledStatus(t *testing.T) {
	_, err := GetEasyTierStatus(context.Background(), nil, true, true)
	if err == nil || !strings.Contains(err.Error(), "no_easytier") {
		t.Fatalf("unexpected disabled status error: %v", err)
	}
}
