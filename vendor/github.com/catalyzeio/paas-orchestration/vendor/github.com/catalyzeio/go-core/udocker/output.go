package udocker

import (
	"bufio"
	"io"
)

type rawReader struct {
	reader *bufio.Reader
	body   io.ReadCloser
}

func newRawReader(body io.ReadCloser) *rawReader {
	reader := bufio.NewReader(body)
	return &rawReader{reader, body}
}

func (r *rawReader) Read() (*OutputLine, error) {
	line, err := r.reader.ReadString('\n')
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// XXX output not tagged; cannot distinguish between stdin and stderr
	return &OutputLine{line, false}, nil
}

func (r *rawReader) Close() error {
	return r.body.Close()
}

type taggedReader struct {
	body io.ReadCloser
}

func newTaggedReader(body io.ReadCloser) *taggedReader {
	return &taggedReader{body}
}

func (t *taggedReader) Read() (*OutputLine, error) {
	header := make([]byte, 8)
	// read header
	_, err := io.ReadFull(t.body, header)
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// read payload
	isStderr := header[0] == 2
	len := uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
	payload := make([]byte, len)
	_, err = io.ReadFull(t.body, payload)
	if err != nil {
		return nil, err
	}
	return &OutputLine{string(payload), isStderr}, nil
}

func (t *taggedReader) Close() error {
	return t.body.Close()
}
