package udocker

import (
	"encoding/json"
	"fmt"
	"io"
)

type Stream struct {
	decoder *json.Decoder
	body    io.ReadCloser
}

func newStream(body io.ReadCloser) *Stream {
	decoder := json.NewDecoder(body)
	return &Stream{decoder, body}
}

func (s *Stream) NextStreamMessage(operation string) (*StreamMessage, error) {
	msg := &StreamMessage{}
	err := s.decoder.Decode(msg)
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(msg.Error) > 0 {
		return msg, fmt.Errorf("%s failed: %s", operation, msg.Error)
	}
	return msg, nil
}

func (s *Stream) NextEventMessage() (*EventMessage, error) {
	msg := &EventMessage{}
	err := s.decoder.Decode(msg)
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func (s *Stream) Close() error {
	return s.body.Close()
}
