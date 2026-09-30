package gobaresip

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

const (
	lengthDelim      byte = ':'
	dataDelim        byte = ','
	maxNetstringSize      = 256 * 1024
)

type reader struct {
	r *bufio.Reader
}

func newReader(r io.Reader) *reader {
	return &reader{r: bufio.NewReader(r)}
}

func (r *reader) readNetstring() ([]byte, error) {
	var header []byte
	for {
		c, err := r.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if c == lengthDelim {
			break
		}
		if c < '0' || c > '9' || len(header) >= 7 {
			return nil, fmt.Errorf("invalid netstring length prefix")
		}
		header = append(header, c)
	}
	lenStr := string(header)
	l, err := strconv.Atoi(lenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid netstring length %q: %w", lenStr, err)
	}
	if l <= 0 || l > maxNetstringSize || (len(header) > 1 && header[0] == '0') {
		return nil, fmt.Errorf("wrong netstring length: %d", l)
	}

	ret := make([]byte, l)
	_, err = io.ReadFull(r.r, ret)
	if err != nil {
		return nil, err
	}
	next, err := r.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if next != dataDelim {
		return nil, fmt.Errorf("invalid netstring terminator %q", next)
	}
	return ret, nil
}
