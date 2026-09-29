package containerbackend

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A single Docker JSON progress object can exceed bufio's 4096-byte read
// buffer (e.g. pacman's whole package list on one line). It must be
// reassembled before parsing; decoding a truncated prefix fails with
// "unexpected end of JSON input" and kills the build.
func TestUnrollStreamLongLine(t *testing.T) {
	longPayload := strings.Repeat("p", 5000)
	input := "{\"stream\":\"line one\"}\n" +
		"{\"stream\":\"" + longPayload + "\"}\n" +
		"{\"stream\":\"line three\"}\n"

	logChan := make(chan string, 16)
	errChan := make(chan error, 1)

	unrollStream(strings.NewReader(input), logChan, errChan)

	require.Empty(t, errChan)
	assert.Equal(t, "line one", <-logChan)
	assert.Equal(t, longPayload, <-logChan)
	assert.Equal(t, "line three", <-logChan)
}
