package gobaresip

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	lengthDelim byte = ':'
	dataDelim   byte = ','
)

type reader struct {
	r *bufio.Reader
}

func newReader(r io.Reader) *reader {
	return &reader{r: bufio.NewReader(r)}
}

func (r *reader) readNetstring() ([]byte, error) {
	raw, err := r.r.ReadBytes(lengthDelim)
	if err != nil {
		return nil, err
	}

	// Trim the trailing ':' delimiter and any surrounding whitespace.
	lenStr := strings.TrimRight(string(raw[:len(raw)-1]), " ")
	l, err := strconv.Atoi(lenStr)
	if err != nil {
		return nil, fmt.Errorf("invalid netstring length %q: %w", lenStr, err)
	}
	if l <= 0 {
		return nil, fmt.Errorf("wrong netstring length: %d", l)
	}

	ret := make([]byte, l)
	_, err = io.ReadFull(r.r, ret)
	if err != nil {
		return nil, err
	}
	next, err := r.r.ReadByte()
	if err != nil && err != io.EOF {
		return nil, err
	}
	if next != dataDelim {
		r.r.UnreadByte()
	}
	return ret, nil
}
