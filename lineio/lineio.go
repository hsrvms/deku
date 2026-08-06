// Package lineio provides a shared non-empty terminal line reader. Both the
// Approval gate and the Agent read user decisions from the same underlying
// reader, so they share one robust implementation instead of each re-implement
// buffer handling and blank-line skipping.
package lineio

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// Scan returns the next non-empty line from br, skipping blank lines. Lines
// longer than the reader's buffer are reassembled whole rather than returned
// in fragments. It reports an error when the input ends (io.EOF) before a
// non-empty line is available or when the read otherwise fails.
func Scan(br *bufio.Reader) (string, error) {
	for {
		line, err := ReadLine(br)
		line = strings.TrimSpace(line)
		if line != "" {
			return line, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", errors.New("input ended before a response")
			}
			return "", err
		}
	}
}

// ReadLine reads one line from br, accumulating fragments so lines longer than
// the reader's buffer are returned whole. It returns io.EOF when the input
// ends with no trailing newline and no partial content is pending.
func ReadLine(br *bufio.Reader) (string, error) {
	var line strings.Builder
	for {
		fragment, err := br.ReadString('\n')
		line.WriteString(fragment)
		if err == nil {
			return line.String(), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && line.Len() > 0 {
			return line.String(), nil
		}
		return line.String(), err
	}
}
