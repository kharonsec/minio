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
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Failed LDAP logins are throttled per user and per client address to
// slow down password guessing. Successful logins do not count. Limits are
// kept in memory on each server.
const (
	ldapFailedLoginsPerUserBurst = 5
	ldapFailedLoginsPerUserEvery = 20 * time.Second
	ldapFailedLoginsPerIPBurst   = 30
	ldapFailedLoginsPerIPEvery   = 2 * time.Second

	// Above this many tracked keys, keys that are no longer
	// throttled are dropped.
	failedLoginLimiterPruneSize = 10000
)

var globalLDAPLoginLimiter = newLoginLimiter()

type loginLimiter struct {
	users *failedLoginLimiter
	ips   *failedLoginLimiter
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		users: newFailedLoginLimiter(rate.Every(ldapFailedLoginsPerUserEvery), ldapFailedLoginsPerUserBurst),
		ips:   newFailedLoginLimiter(rate.Every(ldapFailedLoginsPerIPEvery), ldapFailedLoginsPerIPBurst),
	}
}

// blocked reports whether a login for user from ip must be rejected
// without being attempted.
func (l *loginLimiter) blocked(user, ip string) bool {
	return l.users.blocked(strings.ToLower(user)) || l.ips.blocked(ip)
}

// failed records a failed login for user from ip.
func (l *loginLimiter) failed(user, ip string) {
	l.users.failed(strings.ToLower(user))
	l.ips.failed(ip)
}

type failedLoginLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	limiters map[string]*rate.Limiter
}

func newFailedLoginLimiter(limit rate.Limit, burst int) *failedLoginLimiter {
	return &failedLoginLimiter{
		limit:    limit,
		burst:    burst,
		limiters: make(map[string]*rate.Limiter),
	}
}

func (f *failedLoginLimiter) blocked(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	lim, ok := f.limiters[key]
	return ok && lim.Tokens() < 1
}

func (f *failedLoginLimiter) failed(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lim, ok := f.limiters[key]
	if !ok {
		if len(f.limiters) >= failedLoginLimiterPruneSize {
			f.prune()
		}
		lim = rate.NewLimiter(f.limit, f.burst)
		f.limiters[key] = lim
	}
	lim.Allow()
}

// prune drops limiters that have refilled completely.
func (f *failedLoginLimiter) prune() {
	for key, lim := range f.limiters {
		if lim.Tokens() >= float64(f.burst) {
			delete(f.limiters, key)
		}
	}
}
