// Copyright (c) 2015-2025 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

// encodeUnsignedChunks encodes data as aws-chunked with the given chunk
// sizes, followed by the final chunk and a crc32 trailer.
func encodeUnsignedChunks(data []byte, sizes []int) []byte {
	var b bytes.Buffer
	for _, size := range sizes {
		fmt.Fprintf(&b, "%x\r\n", size)
		b.Write(data[:size])
		b.WriteString("\r\n")
		data = data[size:]
	}
	b.WriteString("0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n")
	return b.Bytes()
}

func newTestUnsignedChunkedReader(t *testing.T, body io.Reader) (io.ReadCloser, *http.Request) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/bucket/object", body)
	req.Header.Set(awsTrailerHeader, "x-amz-checksum-crc32")
	rd, errCode := newUnsignedV4ChunkedReader(req, true)
	if errCode != ErrNone {
		t.Fatalf("unexpected error code %v", errCode)
	}
	return rd, req
}

// Regression test for https://github.com/minio/minio/issues/21611:
// chunks larger than 16 MiB must be accepted.
func TestUnsignedChunkedReaderLargeChunks(t *testing.T) {
	const bigChunk = 17<<20 + 1
	largeSizes := []int{1, 64 << 10, bigChunk, 3, bigChunk}
	testCases := []struct {
		name   string
		sizes  []int
		input  func(io.Reader) io.Reader // wraps the request body
		output func(io.Reader) io.Reader // wraps the chunked reader
	}{
		{"plain", largeSizes, nil, nil},
		{"half reads", largeSizes, nil, iotest.HalfReader},
		{"data with EOF", largeSizes, nil, iotest.DataErrReader},
		{"one byte reads", []int{1, 40, 59}, iotest.OneByteReader, iotest.OneByteReader},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			total := 0
			for _, size := range tc.sizes {
				total += size
			}
			data := make([]byte, total)
			rand.Read(data)

			body := io.Reader(bytes.NewReader(encodeUnsignedChunks(data, tc.sizes)))
			if tc.input != nil {
				body = tc.input(body)
			}
			rd, req := newTestUnsignedChunkedReader(t, body)
			out := io.Reader(rd)
			if tc.output != nil {
				out = tc.output(out)
			}

			got, err := io.ReadAll(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("data mismatch: got %d bytes, want %d", len(got), len(data))
			}
			if v := req.Trailer.Get("x-amz-checksum-crc32"); v != "AAAAAA==" {
				t.Fatalf("unexpected trailer %q", v)
			}
			if err := rd.Close(); err != io.EOF {
				t.Fatalf("unexpected Close error %v", err)
			}
		})
	}
}

// The reader must not buffer whole chunks: memory used must not
// depend on the chunk size.
func TestUnsignedChunkedReaderStreamsChunkData(t *testing.T) {
	const chunkSize = 256 << 20 // 256 MiB
	body := io.MultiReader(
		strings.NewReader(fmt.Sprintf("%x\r\n", chunkSize)),
		io.LimitReader(zeroReader{}, chunkSize),
		strings.NewReader("\r\n0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n"),
	)
	rd, _ := newTestUnsignedChunkedReader(t, body)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	n, err := io.CopyBuffer(io.Discard, rd, make([]byte, 32<<10))
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if n != chunkSize {
		t.Fatalf("read %d bytes, want %d", n, chunkSize)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("allocated %d bytes to read a %d byte chunk", allocated, chunkSize)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestUnsignedChunkedReaderMalformed(t *testing.T) {
	testCases := []struct {
		name string
		body string
		want error
	}{
		{"bad hex", "1g\r\na\r\n0\r\n", errMalformedEncoding},
		{"bad size terminator", "1\ra\r\n", errMalformedEncoding},
		{"bad data terminator", "1\r\naXX0\r\n", errMalformedEncoding},
		{"short data", "10\r\nabc", io.ErrUnexpectedEOF},
		{"missing final chunk", "1\r\na\r\n", io.ErrUnexpectedEOF},
		{"chunk too big", "fffffffffffffff\r\n", errChunkTooBig},
		{"missing trailer", "1\r\na\r\n0\r\n\r\n\r\n", io.ErrUnexpectedEOF},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rd, _ := newTestUnsignedChunkedReader(t, strings.NewReader(tc.body))
			_, err := io.ReadAll(rd)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got error %v, want %v", err, tc.want)
			}
		})
	}
}
