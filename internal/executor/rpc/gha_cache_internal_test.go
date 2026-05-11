package rpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractHost(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"http://host.docker.internal:12345", "host.docker.internal"},
		{"http://10.0.0.1:8080", "10.0.0.1"},
		{"https://secure.example.com:443", "secure.example.com"},
		{"localhost:12345", "localhost"},
		{"192.168.1.1", "192.168.1.1"},
		{"host.docker.internal:12345", "host.docker.internal"},
		{"http://localhost", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			assert.Equal(t, tt.want, extractHost(tt.endpoint))
		})
	}
}
