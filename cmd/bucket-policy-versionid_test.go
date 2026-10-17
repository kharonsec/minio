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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minio/minio/internal/auth"
	"github.com/minio/pkg/v3/policy"
)

// Regression test for https://github.com/minio/minio/issues/21735
func TestConditionValuesVersionIDNull(t *testing.T) {
	// Only allow deleting the latest version (i.e. creating a delete
	// marker), never a specific version.
	const policyJSON = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"AWS": ["*"]},
      "Action": ["s3:DeleteObject"],
      "Resource": ["arn:aws:s3:::bucket/*"],
      "Condition": {"Null": {"s3:versionid": ["true"]}}
    }
  ]
}`
	p, err := policy.ParseBucketPolicyConfig(strings.NewReader(policyJSON), "bucket")
	if err != nil {
		t.Fatal(err)
	}

	isAllowed := func(conds map[string][]string) bool {
		return p.IsAllowed(policy.BucketPolicyArgs{
			Action:          policy.DeleteObjectAction,
			BucketName:      "bucket",
			ObjectName:      "object",
			ConditionValues: conds,
		})
	}

	newReq := func(target string) *http.Request {
		r := httptest.NewRequest(http.MethodDelete, target, nil)
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		return r
	}

	// No versionId: s3:versionid must be absent.
	r := newReq("/bucket/object")
	conds := getConditionValues(r, "", auth.AnonymousCredentials)
	if _, ok := conds["versionid"]; ok {
		t.Fatalf("versionid must not be set when absent, got %v", conds["versionid"])
	}
	if !isAllowed(conds) {
		t.Fatal("expected delete without versionId to be allowed")
	}

	// versionId in the query string.
	r = newReq("/bucket/object?versionId=abc")
	conds = getConditionValues(r, "", auth.AnonymousCredentials)
	if got := conds["versionid"]; len(got) != 1 || got[0] != "abc" {
		t.Fatalf("unexpected versionid %v", got)
	}
	if isAllowed(conds) {
		t.Fatal("expected delete with versionId to be denied")
	}

	// DeleteMultipleObjects: the versionId comes from the request body.
	r = newReq("/bucket?delete=")
	if !isAllowed(getConditionValuesWithVID(r, "", auth.AnonymousCredentials, "")) {
		t.Fatal("expected multi-delete entry without versionId to be allowed")
	}
	if isAllowed(getConditionValuesWithVID(r, "", auth.AnonymousCredentials, "abc")) {
		t.Fatal("expected multi-delete entry with versionId to be denied")
	}
}
