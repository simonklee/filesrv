// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package server

import (
	"net"
	"net/http"

	"github.com/hashicorp/golang-lru"
	"github.com/juju/ratelimit"
	"github.com/simonz05/util/log"
)

// Ratelimiter
type Ratelimiter struct {
	buckets *lru.Cache

	// FillRate fills buckets at the rate of tokens per second up to max
	// capacity.
	FillRate float64

	// Capacity sets max capacity of buckets. See FillRate.
	Capacity int64
}

func NewRatelimiter() *Ratelimiter {
	buckets, _ := lru.New(10000)
	return &Ratelimiter{
		buckets:  buckets,
		FillRate: 1,
		Capacity: 10,
	}
}

// Take takes a token from key's bucket. If there is an available token it
// returns true.
func (r *Ratelimiter) Take(key string) bool {
	v, ok := r.buckets.Get(key)

	var bucket *ratelimit.Bucket

	// new
	if !ok {
		bucket = ratelimit.NewBucketWithRate(r.FillRate, r.Capacity)
		r.buckets.Add(key, bucket)
	} else {
		bucket, _ = v.(*ratelimit.Bucket)
	}

	return bucket.Take(1) == 0
}

var ratelimiter = NewRatelimiter()

// ratelimitHandler wraps an http.Handler with per host request throttling.
// Responds with HTTP 429 when throttled.
func ratelimitHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, err := clientAddr(r)

		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if !ratelimiter.Take(host) {
			log.Println("server: host rate-limited", host)
			http.Error(w, "Too many requests", 429)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func clientAddr(r *http.Request) (string, error) {
	addr := r.Header.Get("X-Forwarded-For")

	if addr != "" {
		return addr, nil
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return host, err
}
