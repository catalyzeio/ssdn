package udocker

import (
	"archive/tar"
	"bytes"
	"time"
)

type TarEntry struct {
	Header *tar.Header
	Data   []byte
}

func NewTarEntry(name string, data []byte, mode int64) *TarEntry {
	return &TarEntry{
		Header: &tar.Header{
			Name:     name,
			Mode:     mode,
			Typeflag: tar.TypeReg,
			ModTime:  time.Now(),
			Size:     int64(len(data)),
		},
		Data: data,
	}
}

func TarBuffer(entries []*TarEntry) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	w := tar.NewWriter(buf)
	for _, entry := range entries {
		if err := w.WriteHeader(entry.Header); err != nil {
			return nil, err
		}
		if _, err := w.Write(entry.Data); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func TarBufferFromMap(files map[string][]byte) (*bytes.Buffer, error) {
	entries := make([]*TarEntry, 0, len(files))
	for name, data := range files {
		entries = append(entries, NewTarEntry(name, data, 0600))
	}
	return TarBuffer(entries)
}
