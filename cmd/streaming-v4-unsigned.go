// Copyright (c) 2015-2023 MinIO, Inc.
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
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	xhttp "github.com/minio/minio/internal/http"
)

// newUnsignedV4ChunkedReader returns a new s3UnsignedChunkedReader that translates the data read from r
// out of HTTP "chunked" format before returning it.
// The s3ChunkedReader returns io.EOF when the final 0-length chunk is read.
//
// Requests without credentials are anonymous and are not verified. Any
// credentials present, in the Authorization header or in the query
// string, must carry a valid signature.
func newUnsignedV4ChunkedReader(req *http.Request, trailer bool) (io.ReadCloser, APIErrorCode) {
	var errCode APIErrorCode
	switch {
	case isRequestSignatureV4(req):
		errCode = doesSignatureMatch(unsignedPayloadTrailer, req, globalSite.Region(), serviceS3)
	case isRequestPresignedSignatureV4(req):
		errCode = doesPresignedSignatureMatch(getContentSha256Cksum(req, serviceS3), req, globalSite.Region(), serviceS3)
	case req.Header.Get(xhttp.Authorization) != "":
		errCode = ErrSignatureVersionNotSupported
	}
	if errCode != ErrNone {
		return nil, errCode
	}
	if trailer {
		// Discard anything unsigned.
		req.Trailer = make(http.Header)
		trailers := req.Header.Values(awsTrailerHeader)
		for _, key := range trailers {
			req.Trailer.Add(key, "")
		}
	} else {
		req.Trailer = nil
	}
	return &s3UnsignedChunkedReader{
		trailers: req.Trailer,
		reader:   bufio.NewReader(req.Body),
	}, ErrNone
}

// maxUnsignedChunkSize is the largest chunk accepted by
// s3UnsignedChunkedReader. Chunk data is streamed rather than
// buffered, so the limit only guards the size parsing against
// overflow: no chunk can be larger than an object.
const maxUnsignedChunkSize = globalMaxObjectSize

// Represents the overall state that is required for decoding a
// AWS Signature V4 chunked reader.
type s3UnsignedChunkedReader struct {
	reader   *bufio.Reader
	trailers http.Header

	// remaining is the number of data bytes of the current
	// chunk not yet returned to the caller.
	remaining int64
	err       error
	debug     bool
}

func (cr *s3UnsignedChunkedReader) Close() (err error) {
	return cr.err
}

// mustRead reads from input and compares against provided slice.
func (cr *s3UnsignedChunkedReader) mustRead(b ...byte) error {
	for _, want := range b {
		got, err := cr.reader.ReadByte()
		if err == io.EOF {
			return io.ErrUnexpectedEOF
		}
		if err != nil {
			return err
		}
		if got != want {
			if cr.debug {
				fmt.Printf("mustread: want: %q got: %q\n", string(want), string(got))
			}
			return errMalformedEncoding
		}
	}
	return nil
}

// readChunkSize reads a chunk size line, terminated by \r\n.
func (cr *s3UnsignedChunkedReader) readChunkSize() (int64, error) {
	var size int64
	for {
		b, err := cr.reader.ReadByte()
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return 0, err
		}
		if b == '\r' { // \r\n denotes end of size.
			if err := cr.mustRead('\n'); err != nil {
				return 0, err
			}
			return size, nil
		}

		// Manually deserialize the size since AWS specified
		// the chunk size to be of variable width. In particular,
		// a size of 16 is encoded as `10` while a size of 64 KB
		// is `10000`.
		switch {
		case b >= '0' && b <= '9':
			size = size<<4 | int64(b-'0')
		case b >= 'a' && b <= 'f':
			size = size<<4 | int64(b-('a'-10))
		case b >= 'A' && b <= 'F':
			size = size<<4 | int64(b-('A'-10))
		default:
			if cr.debug {
				fmt.Printf("err size: %v\n", string(b))
			}
			return 0, errMalformedEncoding
		}
		if size > maxUnsignedChunkSize {
			return 0, errChunkTooBig
		}
	}
}

// Read - implements `io.Reader`, which transparently decodes
// the incoming AWS Signature V4 streaming signature.
//
// Chunk data is copied straight from the request body to buf, so the
// memory used does not depend on the chunk size.
func (cr *s3UnsignedChunkedReader) Read(buf []byte) (n int, err error) {
	if cr.err != nil {
		return 0, cr.err
	}
	for n < len(buf) {
		if cr.remaining == 0 {
			if n > 0 {
				// Return what we have before blocking on the next chunk.
				return n, nil
			}
			size, err := cr.readChunkSize()
			if err != nil {
				cr.err = err
				return n, err
			}
			// If the chunk size is zero we return io.EOF. As specified by AWS,
			// only the last chunk is zero-sized.
			if size == 0 {
				if cr.debug {
					fmt.Println("EOF")
				}
				if cr.trailers != nil {
					err = cr.readTrailers()
					if cr.debug {
						fmt.Println("trailer returned:", err)
					}
					if err != nil {
						cr.err = err
						return 0, err
					}
				}
				cr.err = io.EOF
				return n, cr.err
			}
			cr.remaining = size
		}

		// Now, we read the payload.
		p := buf[n:]
		if int64(len(p)) > cr.remaining {
			p = p[:cr.remaining]
		}
		m, err := cr.reader.Read(p)
		n += m
		cr.remaining -= int64(m)
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			cr.err = err
			return n, err
		}
		if cr.remaining == 0 {
			// read final terminator.
			if err := cr.mustRead('\r', '\n'); err != nil {
				cr.err = err
				return n, err
			}
		}
	}
	return n, nil
}

// readTrailers will read all trailers and populate cr.trailers with actual values.
func (cr *s3UnsignedChunkedReader) readTrailers() error {
	var valueBuffer bytes.Buffer
	// Read value
	for {
		v, err := cr.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
		}
		if v != '\r' {
			valueBuffer.WriteByte(v)
			continue
		}
		// Must end with \r\n\r\n
		var tmp [3]byte
		_, err = io.ReadFull(cr.reader, tmp[:])
		if err != nil {
			if err == io.EOF {
				return io.ErrUnexpectedEOF
			}
		}
		if !bytes.Equal(tmp[:], []byte{'\n', '\r', '\n'}) {
			if cr.debug {
				fmt.Printf("got %q, want %q\n", string(tmp[:]), "\n\r\n")
			}
			return errMalformedEncoding
		}
		break
	}

	// Parse trailers.
	wantTrailers := make(map[string]struct{}, len(cr.trailers))
	for k := range cr.trailers {
		wantTrailers[strings.ToLower(k)] = struct{}{}
	}
	input := bufio.NewScanner(bytes.NewReader(valueBuffer.Bytes()))
	for input.Scan() {
		line := strings.TrimSpace(input.Text())
		if line == "" {
			continue
		}
		// Find first separator.
		idx := strings.IndexByte(line, trailerKVSeparator[0])
		if idx <= 0 || idx >= len(line) {
			if cr.debug {
				fmt.Printf("Could not find separator, got %q\n", line)
			}
			return errMalformedEncoding
		}
		key := strings.ToLower(line[:idx])
		value := line[idx+1:]
		if _, ok := wantTrailers[key]; !ok {
			if cr.debug {
				fmt.Printf("Unknown key %q - expected on of %v\n", key, cr.trailers)
			}
			return errMalformedEncoding
		}
		cr.trailers.Set(key, value)
		delete(wantTrailers, key)
	}

	// Check if we got all we want.
	if len(wantTrailers) > 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}
