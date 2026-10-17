// Copyright (c) 2015-2021 MinIO, Inc.
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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	xhttp "github.com/minio/minio/internal/http"
)

// Tests - canonicalizeETag()
func TestCanonicalizeETag(t *testing.T) {
	testCases := []struct {
		etag              string
		canonicalizedETag string
	}{
		{
			etag:              "\"\"\"",
			canonicalizedETag: "",
		},
		{
			etag:              "\"\"\"abc\"",
			canonicalizedETag: "abc",
		},
		{
			etag:              "abcd",
			canonicalizedETag: "abcd",
		},
		{
			etag:              "abcd\"\"",
			canonicalizedETag: "abcd",
		},
	}
	for _, test := range testCases {
		etag := canonicalizeETag(test.etag)
		if test.canonicalizedETag != etag {
			t.Fatalf("Expected %s , got %s", test.canonicalizedETag, etag)
		}
	}
}

// Tests - CheckPreconditions()
func TestCheckPreconditions(t *testing.T) {
	objModTime := time.Date(2024, time.August, 26, 0o2, 0o1, 0o1, 0, time.UTC)
	objInfo := ObjectInfo{ETag: "aa", ModTime: objModTime}
	testCases := []struct {
		name              string
		ifMatch           string
		ifNoneMatch       string
		ifModifiedSince   string
		ifUnmodifiedSince string
		objInfo           ObjectInfo
		expectedFlag      bool
		expectedCode      int
	}{
		// If-None-Match(false) and If-Modified-Since(true)
		{
			name:            "If-None-Match1",
			ifNoneMatch:     "aa",
			ifModifiedSince: "Sun, 26 Aug 2024 02:01:00 GMT",
			objInfo:         objInfo,
			expectedFlag:    true,
			expectedCode:    304,
		},
		// If-Modified-Since(false)
		{
			name:            "If-None-Match2",
			ifNoneMatch:     "aaa",
			ifModifiedSince: "Sun, 26 Aug 2024 02:01:01 GMT",
			objInfo:         objInfo,
			expectedFlag:    true,
			expectedCode:    304,
		},
		{
			name:            "If-None-Match3",
			ifNoneMatch:     "aaa",
			ifModifiedSince: "Sun, 26 Aug 2024 02:01:02 GMT",
			objInfo:         objInfo,
			expectedFlag:    true,
			expectedCode:    304,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodHead, "/bucket/a", bytes.NewReader([]byte{}))
			request.Header.Set(xhttp.IfNoneMatch, tc.ifNoneMatch)
			request.Header.Set(xhttp.IfModifiedSince, tc.ifModifiedSince)
			request.Header.Set(xhttp.IfMatch, tc.ifMatch)
			request.Header.Set(xhttp.IfUnmodifiedSince, tc.ifUnmodifiedSince)
			actualFlag := checkPreconditions(t.Context(), recorder, request, tc.objInfo, ObjectOptions{})
			if tc.expectedFlag != actualFlag {
				t.Errorf("test: %s, got flag: %v, want: %v", tc.name, actualFlag, tc.expectedFlag)
			}
			if tc.expectedCode != recorder.Code {
				t.Errorf("test: %s, got code: %d, want: %d", tc.name, recorder.Code, tc.expectedCode)
			}
		})
	}
	testCases = []struct {
		name              string
		ifMatch           string
		ifNoneMatch       string
		ifModifiedSince   string
		ifUnmodifiedSince string
		objInfo           ObjectInfo
		expectedFlag      bool
		expectedCode      int
	}{
		// If-Match(true) and If-Unmodified-Since(false)
		{
			name:              "If-Match1",
			ifMatch:           "aa",
			ifUnmodifiedSince: "Sun, 26 Aug 2024 02:01:00 GMT",
			objInfo:           objInfo,
			expectedFlag:      false,
			expectedCode:      200,
		},
		// If-Unmodified-Since(true)
		{
			name:              "If-Match2",
			ifMatch:           "aa",
			ifUnmodifiedSince: "Sun, 26 Aug 2024 02:01:01 GMT",
			objInfo:           objInfo,
			expectedFlag:      false,
			expectedCode:      200,
		},
		{
			name:              "If-Match3",
			ifMatch:           "aa",
			ifUnmodifiedSince: "Sun, 26 Aug 2024 02:01:02 GMT",
			objInfo:           objInfo,
			expectedFlag:      false,
			expectedCode:      200,
		},
		// If-Match(true)
		{
			name:         "If-Match4",
			ifMatch:      "aa",
			objInfo:      objInfo,
			expectedFlag: false,
			expectedCode: 200,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodHead, "/bucket/a", bytes.NewReader([]byte{}))
			request.Header.Set(xhttp.IfNoneMatch, tc.ifNoneMatch)
			request.Header.Set(xhttp.IfModifiedSince, tc.ifModifiedSince)
			request.Header.Set(xhttp.IfMatch, tc.ifMatch)
			request.Header.Set(xhttp.IfUnmodifiedSince, tc.ifUnmodifiedSince)
			actualFlag := checkPreconditions(t.Context(), recorder, request, tc.objInfo, ObjectOptions{})
			if tc.expectedFlag != actualFlag {
				t.Errorf("test: %s, got flag: %v, want: %v", tc.name, actualFlag, tc.expectedFlag)
			}
			if tc.expectedCode != recorder.Code {
				t.Errorf("test: %s, got code: %d, want: %d", tc.name, recorder.Code, tc.expectedCode)
			}
		})
	}
}

// Regression test for https://github.com/minio/minio/issues/21727
func TestIsETagEqual(t *testing.T) {
	testCases := []struct {
		etag   string
		header string
		want   bool
	}{
		{"aa", "aa", true},
		{"aa", `"aa"`, true},
		{"aa", "bb", false},
		{"aa", "*", true},
		{"aa", " * ", true},
		{"aa", `"bb", "aa"`, true},
		{"aa", `"bb","aa"`, true},
		{"aa", `"bb" , "cc"`, false},
		{"aa", `W/"aa"`, true},
		{"aa", `"bb", W/"aa"`, true},
		{"aa", `"bb", *`, true},
		{"aa", "", false},
		{"aa", ",", false},
		{"aa-2", `"bb", "aa-2"`, true},
	}
	for _, tc := range testCases {
		if got := isETagEqual(tc.etag, tc.header); got != tc.want {
			t.Errorf("isETagEqual(%q, %q) = %v, want %v", tc.etag, tc.header, got, tc.want)
		}
	}
}

// Regression test for https://github.com/minio/minio/issues/21727
func TestCheckPreconditionsETagList(t *testing.T) {
	objInfo := ObjectInfo{ETag: "aa", ModTime: time.Date(2024, time.August, 26, 0o2, 0o1, 0o1, 0, time.UTC)}
	testCases := []struct {
		name         string
		method       string
		header       string
		value        string
		expectedFlag bool
		expectedCode int
	}{
		{"GET If-None-Match list matches", http.MethodGet, xhttp.IfNoneMatch, `"bb", "aa"`, true, http.StatusNotModified},
		{"GET If-None-Match list no match", http.MethodGet, xhttp.IfNoneMatch, `"bb", "cc"`, false, http.StatusOK},
		{"GET If-Match list matches", http.MethodGet, xhttp.IfMatch, `"bb", "aa"`, false, http.StatusOK},
		{"GET If-Match list no match", http.MethodGet, xhttp.IfMatch, `"bb", "cc"`, true, http.StatusPreconditionFailed},
		{"PUT If-None-Match list matches", http.MethodPut, xhttp.IfNoneMatch, `"bb", "aa"`, true, http.StatusPreconditionFailed},
		{"PUT If-None-Match list no match", http.MethodPut, xhttp.IfNoneMatch, `"bb", "cc"`, false, http.StatusOK},
		{"PUT If-Match list matches", http.MethodPut, xhttp.IfMatch, `"bb", "aa"`, false, http.StatusOK},
		{"PUT If-Match list no match", http.MethodPut, xhttp.IfMatch, `"bb", "cc"`, true, http.StatusPreconditionFailed},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, "/bucket/a", bytes.NewReader([]byte{}))
			request.Header.Set(tc.header, tc.value)
			var flag bool
			if tc.method == http.MethodPut {
				flag = checkPreconditionsPUT(t.Context(), recorder, request, objInfo, ObjectOptions{})
			} else {
				flag = checkPreconditions(t.Context(), recorder, request, objInfo, ObjectOptions{})
			}
			if flag != tc.expectedFlag {
				t.Errorf("got flag: %v, want: %v", flag, tc.expectedFlag)
			}
			if recorder.Code != tc.expectedCode {
				t.Errorf("got code: %d, want: %d", recorder.Code, tc.expectedCode)
			}
		})
	}
}
