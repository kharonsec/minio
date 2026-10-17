// Copyright (c) 2015-2026 MinIO, Inc.
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
	"strings"
	"testing"

	"github.com/minio/minio/internal/auth"
	"github.com/minio/pkg/v3/policy"
)

// Policy conditions on s3:versionid must see the version ID of the object
// being deleted, and Null must match requests without a version ID.
func TestConditionValuesVersionID(t *testing.T) {
	// Allow everything, but deny deleting objects without a version ID.
	p, err := policy.ParseConfig(strings.NewReader(`{
  "Version": "2012-10-17",
  "Statement": [
    {"Effect": "Allow", "Action": ["s3:*"], "Resource": ["arn:aws:s3:::bucket/*"]},
    {"Effect": "Deny", "Action": ["s3:DeleteObject"], "Resource": ["arn:aws:s3:::bucket/*"],
     "Condition": {"Null": {"s3:versionid": "true"}}}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	cred := auth.Credentials{AccessKey: "user"}
	allowed := func(conditions map[string][]string) bool {
		return p.IsAllowed(policy.Args{
			AccountName:     cred.AccessKey,
			Action:          policy.DeleteObjectAction,
			BucketName:      "bucket",
			ObjectName:      "object",
			ConditionValues: conditions,
		})
	}
	newReq := func(rawURL string) *http.Request {
		r := mustNewRequest(http.MethodDelete, rawURL, 0, nil, t)
		getRequestAuthType(r) // parses r.Form
		return r
	}

	if allowed(getConditionValues(newReq("http://localhost:9000/bucket/object"), "", cred)) {
		t.Error("delete without a version ID was allowed")
	}
	if !allowed(getConditionValues(newReq("http://localhost:9000/bucket/object?versionId=v1"), "", cred)) {
		t.Error("delete with a version ID in the URL was denied")
	}

	// DeleteObjects: the version ID comes from the request body.
	bulk := newReq("http://localhost:9000/bucket?delete=")
	if !allowed(getConditionValuesWithVID(bulk, "", cred, "v1")) {
		t.Error("bulk delete of a specific version was denied")
	}
	if allowed(getConditionValuesWithVID(bulk, "", cred, "")) {
		t.Error("bulk delete without a version ID was allowed")
	}
}
