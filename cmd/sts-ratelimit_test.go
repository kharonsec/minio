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
	"fmt"
	"testing"
)

func TestLoginLimiter(t *testing.T) {
	l := newLoginLimiter()

	// Failures for one user eventually block that user, from any address,
	// but not other users.
	for i := range ldapFailedLoginsPerUserBurst {
		if l.blocked("alice", fmt.Sprintf("10.0.0.%d", i)) {
			t.Fatalf("alice blocked after %d failures", i)
		}
		l.failed("alice", fmt.Sprintf("10.0.0.%d", i))
	}
	if !l.blocked("ALICE", "10.0.1.1") {
		t.Fatal("alice not blocked after repeated failures")
	}
	if l.blocked("bob", "10.0.1.1") {
		t.Fatal("bob blocked by alice's failures")
	}

	// Failures across many users from one address block that address.
	for i := range ldapFailedLoginsPerIPBurst {
		l.failed(fmt.Sprintf("user%d", i), "192.0.2.1")
	}
	if !l.blocked("carol", "192.0.2.1") {
		t.Fatal("address not blocked after repeated failures")
	}
	if l.blocked("carol", "192.0.2.2") {
		t.Fatal("other address blocked")
	}
}

func TestFailedLoginLimiterPrune(t *testing.T) {
	f := newFailedLoginLimiter(1000, 1)
	for i := range failedLoginLimiterPruneSize + 10 {
		f.failed(fmt.Sprint(i))
	}
	if len(f.limiters) > failedLoginLimiterPruneSize {
		t.Fatalf("limiters not pruned: %d", len(f.limiters))
	}
}
