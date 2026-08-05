package ingest

import (
	"compress/gzip"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

func openDecompressed(source io.Reader, compression string) (io.ReadCloser, error) {
	switch compression {
	case "":
		return io.NopCloser(source), nil
	case "gzip":
		reader, err := gzip.NewReader(source)
		if err != nil {
			return nil, err
		}
		return reader, nil
	case "zstd":
		reader, err := zstd.NewReader(source,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(256<<20),
		)
		if err != nil {
			return nil, err
		}
		return readCloser{Reader: reader, close: func() error {
			reader.Close()
			return nil
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported JSONL compression %q (supported: gzip, zstd)", compression)
	}
}

type readCloser struct {
	io.Reader
	close func() error
}

func (reader readCloser) Close() error { return reader.close() }
