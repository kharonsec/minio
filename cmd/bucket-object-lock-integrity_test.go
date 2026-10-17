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
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	xhttp "github.com/minio/minio/internal/http"
)

// Regression test for https://github.com/minio/minio/issues/16480:
// uploads under an object lock retention period must carry Content-MD5
// or an x-amz-checksum-* header.
func TestObjectLockRequiresIntegrityHeader(t *testing.T) {
	ts := StartTestServer(t, "ErasureSD")
	defer ts.Stop()

	savedStrict := globalServerCtxt.StrictS3Compat
	globalServerCtxt.StrictS3Compat = true
	defer func() { globalServerCtxt.StrictS3Compat = savedStrict }()

	client := ts.Server.Client()
	payload := []byte("hello world")

	// do sends a V4 signed request. Content-MD5 is only kept if keepMD5 is set.
	do := func(method, target string, body []byte, keepMD5 bool, headers map[string]string) *http.Response {
		t.Helper()
		var rd io.ReadSeeker
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := newTestRequest(method, target, int64(len(body)), rd)
		if err != nil {
			t.Fatal(err)
		}
		if !keepMD5 {
			req.Header.Del(xhttp.ContentMD5)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if err = signRequestV4(req, ts.AccessKey, ts.SecretKey); err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	expect := func(resp *http.Response, status int, code string) []byte {
		t.Helper()
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("got status %d, want %d: %s", resp.StatusCode, status, b)
		}
		if code != "" && !bytes.Contains(b, []byte("<Code>"+code+"</Code>")) {
			t.Fatalf("expected error code %s, got %s", code, b)
		}
		return b
	}

	bucket := getRandomBucketName()
	expect(do(http.MethodPut, getMakeBucketURL(ts.Server.URL, bucket), nil, false,
		map[string]string{xhttp.AmzObjectLockEnabled: "true"}), http.StatusOK, "")

	retention := map[string]string{
		xhttp.AmzObjectLockMode:            "GOVERNANCE",
		xhttp.AmzObjectLockRetainUntilDate: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	withHeader := func(k, v string) map[string]string {
		m := map[string]string{k: v}
		for rk, rv := range retention {
			m[rk] = rv
		}
		return m
	}
	objURL := getPutObjectURL(ts.Server.URL, bucket, "object")

	// No retention: no integrity header needed.
	expect(do(http.MethodPut, objURL, payload, false, nil), http.StatusOK, "")

	// Explicit retention.
	expect(do(http.MethodPut, objURL, payload, false, retention), http.StatusBadRequest, "MissingContentMD5")
	expect(do(http.MethodPut, objURL, payload, true, retention), http.StatusOK, "")
	expect(do(http.MethodPut, objURL, payload, false,
		withHeader("x-amz-checksum-crc32", "DUoRhQ==")), http.StatusOK, "")

	// --no-compat disables the check.
	globalServerCtxt.StrictS3Compat = false
	expect(do(http.MethodPut, objURL, payload, false, retention), http.StatusOK, "")
	globalServerCtxt.StrictS3Compat = true

	// Multipart upload created with retention: parts need an integrity header.
	b := expect(do(http.MethodPost, getNewMultipartURL(ts.Server.URL, bucket, "mpobject"), nil, false, retention), http.StatusOK, "")
	var initResp InitiateMultipartUploadResponse
	if err := xml.Unmarshal(b, &initResp); err != nil {
		t.Fatal(err)
	}
	partURL := getPutObjectPartURL(ts.Server.URL, bucket, "mpobject", initResp.UploadID, "1")
	expect(do(http.MethodPut, partURL, payload, false, nil), http.StatusBadRequest, "MissingContentMD5")
	expect(do(http.MethodPut, partURL, payload, true, nil), http.StatusOK, "")

	// Bucket default retention.
	lockConfig := `<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>1</Days></DefaultRetention></Rule></ObjectLockConfiguration>`
	lockURL := makeTestTargetURL(ts.Server.URL, bucket, "", url.Values{"object-lock": []string{""}})
	expect(do(http.MethodPut, lockURL, []byte(lockConfig), true, nil), http.StatusOK, "")

	expect(do(http.MethodPut, objURL, payload, false, nil), http.StatusBadRequest, "MissingContentMD5")
	expect(do(http.MethodPut, objURL, payload, true, nil), http.StatusOK, "")

	// Multipart upload inheriting the bucket default retention.
	b = expect(do(http.MethodPost, getNewMultipartURL(ts.Server.URL, bucket, "mpobject2"), nil, false, nil), http.StatusOK, "")
	if err := xml.Unmarshal(b, &initResp); err != nil {
		t.Fatal(err)
	}
	partURL = getPutObjectPartURL(ts.Server.URL, bucket, "mpobject2", initResp.UploadID, "1")
	expect(do(http.MethodPut, partURL, payload, false, nil), http.StatusBadRequest, "MissingContentMD5")
	expect(do(http.MethodPut, partURL, payload, false, map[string]string{"x-amz-checksum-crc32": "DUoRhQ=="}), http.StatusOK, "")
}
