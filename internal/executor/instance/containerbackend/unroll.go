package containerbackend

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func unrollStream(reader io.Reader, logChan chan<- string, errChan chan<- error) {
	buildProgressReader := bufio.NewReader(reader)

	for {
		// Docker build progress is line-based, but a single JSON object
		// can exceed the reader's buffer (e.g. pacman's whole package
		// list on one line), so accumulate fragments until a full line
		// is available instead of parsing a truncated prefix.
		var line []byte
		for {
			fragment, isPrefix, err := buildProgressReader.ReadLine()
			if err != nil {
				if err != io.EOF {
					errChan <- err
					return
				}
				// EOF: parse any trailing bytes, then finish.
				break
			}
			line = append(line, fragment...)
			if !isPrefix {
				break
			}
		}
		if len(line) == 0 {
			break
		}

		// Each line is a JSON object with the actual message wrapped in it
		msg := &struct {
			Stream      string
			ErrorDetail struct {
				Message string
			}
		}{}
		if err := json.Unmarshal(line, &msg); err != nil {
			errChan <- fmt.Errorf("%w (raw: %q, len: %d, hex: %x)", err, string(line), len(line), line)
			return
		}

		if msg.ErrorDetail.Message != "" {
			errChan <- fmt.Errorf("%w: %s", ErrBuildFailed, msg.ErrorDetail.Message)
		}

		// We're only interested with messages containing the "stream" field, as these are the most helpful
		if msg.Stream == "" {
			continue
		}

		// Cut the unnecessary formatting done by the Docker daemon for some reason
		progressMessage := strings.TrimSpace(msg.Stream)

		// Some messages contain only "\n", so filter these out
		if progressMessage == "" {
			continue
		}

		logChan <- progressMessage
	}
}
