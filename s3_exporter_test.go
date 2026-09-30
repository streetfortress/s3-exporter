// Copyright 2018-2021 Rob Best (ribbybibby) and the s3_exporter contributors.
// Copyright 2026 Streetfortress Industries.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Streetfortress Industries changed this file. It comes from
// github.com/ribbybibby/s3_exporter. See NOTICE.

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var (
	mockSvc    = &mockS3Client{}
	testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))
	// The probe timeout the tests pass when the timeout is not the subject.
	testTimeout = probeTimeout{fallback: 2 * time.Minute, offset: 500 * time.Millisecond}
	testCases   = s3ExporterTestCases{
		// Test one object in a bucket
		s3ExporterTestCase{
			Name:   "one object",
			Bucket: "mock",
			Prefix: "one",
			ExpectedOutputLines: []string{
				"s3_list_success{bucket=\"mock\",delimiter=\"\",prefix=\"one\"} 1",
				"s3_last_modified_object_date{bucket=\"mock\",prefix=\"one\"} 1.5604596e+09",
				"s3_last_modified_object_size_bytes{bucket=\"mock\",prefix=\"one\"} 1234",
				"s3_biggest_object_size_bytes{bucket=\"mock\",prefix=\"one\"} 1234",
				"s3_objects_size_sum_bytes{bucket=\"mock\",prefix=\"one\"} 1234",
				"s3_objects{bucket=\"mock\",prefix=\"one\"} 1",
			},
			ListObjectsV2Response: &s3.ListObjectsV2Output{
				Contents: []types.Object{
					types.Object{
						Key:          String("one"),
						LastModified: Time(time.Date(2019, time.June, 13, 21, 0, 0, 0, time.UTC)),
						Size:         Int64(1234),
					},
				},
				IsTruncated: Bool(false),
				KeyCount:    Int32(1),
				MaxKeys:     Int32(1000),
				Name:        String("mock"),
				Prefix:      String("one"),
			},
		},
		// Test no matching objects in the bucket
		s3ExporterTestCase{
			Name:   "no objects",
			Bucket: "mock",
			Prefix: "none",
			ExpectedOutputLines: []string{
				"s3_biggest_object_size_bytes{bucket=\"mock\",prefix=\"none\"} 0",
				"s3_last_modified_object_date{bucket=\"mock\",prefix=\"none\"} -6.795364578e+09",
				"s3_last_modified_object_size_bytes{bucket=\"mock\",prefix=\"none\"} 0",
				"s3_list_success{bucket=\"mock\",delimiter=\"\",prefix=\"none\"} 1",
				"s3_objects_size_sum_bytes{bucket=\"mock\",prefix=\"none\"} 0",
				"s3_objects{bucket=\"mock\",prefix=\"none\"} 0",
			},
			ListObjectsV2Response: &s3.ListObjectsV2Output{
				Contents:    []types.Object{},
				IsTruncated: Bool(false),
				KeyCount:    Int32(0),
				MaxKeys:     Int32(1000),
				Name:        String("mock"),
				Prefix:      String("none"),
			},
		},
		// Test multiple objects
		s3ExporterTestCase{
			Name:   "multiple objects",
			Bucket: "mock",
			Prefix: "multiple",
			ExpectedOutputLines: []string{
				"s3_biggest_object_size_bytes{bucket=\"mock\",prefix=\"multiple\"} 4567",
				"s3_last_modified_object_date{bucket=\"mock\",prefix=\"multiple\"} 1.568592e+09",
				"s3_last_modified_object_size_bytes{bucket=\"mock\",prefix=\"multiple\"} 4567",
				"s3_list_success{bucket=\"mock\",delimiter=\"\",prefix=\"multiple\"} 1",
				"s3_objects_size_sum_bytes{bucket=\"mock\",prefix=\"multiple\"} 11602",
				"s3_objects{bucket=\"mock\",prefix=\"multiple\"} 4",
			},
			ListObjectsV2Response: &s3.ListObjectsV2Output{
				Contents: []types.Object{
					types.Object{
						Key:          String("multiple0"),
						LastModified: Time(time.Date(2019, time.June, 13, 21, 0, 0, 0, time.UTC)),
						Size:         Int64(1234),
					},
					types.Object{
						Key:          String("multiple1"),
						LastModified: Time(time.Date(2019, time.July, 14, 22, 0, 0, 0, time.UTC)),
						Size:         Int64(2345),
					},
					types.Object{
						Key:          String("multiple2"),
						LastModified: Time(time.Date(2019, time.August, 15, 23, 0, 0, 0, time.UTC)),
						Size:         Int64(3456),
					},
					types.Object{
						Key:          String("multiple/0"),
						LastModified: Time(time.Date(2019, time.September, 16, 00, 0, 0, 0, time.UTC)),
						Size:         Int64(4567),
					},
				},
				IsTruncated: Bool(false),
				KeyCount:    Int32(4),
				MaxKeys:     Int32(1000),
				Name:        String("mock"),
				Prefix:      String("multiple"),
			},
		},
		// Test delimiter
		s3ExporterTestCase{
			Name:      "common prefixes",
			Bucket:    "mock",
			Prefix:    "mock-prefix",
			Delimiter: "/",
			ExpectedOutputLines: []string{
				"s3_list_success{bucket=\"mock\",delimiter=\"/\",prefix=\"mock-prefix\"} 1",
				"s3_common_prefixes{bucket=\"mock\",delimiter=\"/\",prefix=\"mock-prefix\"} 3",
			},
			ListObjectsV2Response: &s3.ListObjectsV2Output{
				Name:   aws.String("mock"),
				Prefix: aws.String("mock-prefix"),
				CommonPrefixes: []types.CommonPrefix{
					{
						Prefix: aws.String("one"),
					},
					{
						Prefix: aws.String("two"),
					},
					{
						Prefix: aws.String("three"),
					},
				},
			},
		},
	}
)

type mockS3Client struct{}

type s3ExporterTestCase struct {
	Name                  string
	Bucket                string
	Prefix                string
	Delimiter             string
	ExpectedOutputLines   []string
	ListObjectsV2Response *s3.ListObjectsV2Output
}

// testBody tests the body returned by the exporter against the expected output
func (tc *s3ExporterTestCase) testBody(body string, t *testing.T) {
	for _, l := range tc.ExpectedOutputLines {
		ok := strings.Contains(body, l)
		if !ok {
			t.Errorf("expected %s", l)
		}
	}
}

type s3ExporterTestCases []s3ExporterTestCase

// Returns the mocked response for a bucket+prefix combination
func (tcs *s3ExporterTestCases) response(bucket, prefix string) (*s3.ListObjectsV2Output, error) {
	for _, c := range *tcs {
		if c.Bucket == bucket && c.Prefix == prefix {
			return c.ListObjectsV2Response, nil
		}
	}

	return nil, errors.New("Can't find a response for the bucket and prefix combination")
}

// TestProbeHandler iterates over a list of test cases
// The prometheus-operator Probe CR passes one parameter, `target`; the
// exporter reads it as bucket/prefix and answers exactly as for the
// two-parameter form.
func TestProbeTargetForm(t *testing.T) {
	c := testCases[0]
	req, _ := http.NewRequest("GET", "/probe?target="+c.Bucket+"/"+c.Prefix, nil)
	rr := httptest.NewRecorder()
	probeHandler(rr, req, mockSvc, testLogger, testTimeout)
	c.testBody(rr.Body.String(), t)

	for _, bad := range []string{"/probe", "/probe?target=", "/probe?target=mock/one&bucket=mock"} {
		req, _ := http.NewRequest("GET", bad, nil)
		rr := httptest.NewRecorder()
		probeHandler(rr, req, mockSvc, testLogger, testTimeout)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, rr.Code)
		}
	}
}

// A refused listing must come back as s3_list_success 0 — the sample the
// caps/wrong-key case exists to produce. Upstream panicked here instead.
func TestListFailureIsAMetricNotAPanic(t *testing.T) {
	rr, err := probe("mock", "does-not-exist", "")
	if err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `s3_list_success{bucket="mock",delimiter="",prefix="does-not-exist"} 0`) {
		t.Errorf("want s3_list_success 0 in a 200 body, got %d:\n%s", rr.Code, rr.Body.String())
	}
}

func TestProbeHandler(t *testing.T) {
	for _, c := range testCases {
		rr, err := probe(c.Bucket, c.Prefix, c.Delimiter)
		if err != nil {
			t.Error(err)
		}

		c.testBody(rr.Body.String(), t)
	}
}

// ListObjectsV2 mocks out the corresponding function in the S3 client, returning the response that corresponds to the test case
func (m *mockS3Client) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	r, err := testCases.response(*input.Bucket, *input.Prefix)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// Repeatable probe function
func probe(bucket, prefix, delimiter string) (rr *httptest.ResponseRecorder, err error) {
	uri := "/probe?bucket=" + bucket
	if len(prefix) > 0 {
		uri = uri + "&prefix=" + prefix
	}
	if len(delimiter) > 0 {
		uri = uri + "&delimiter=" + delimiter
	}
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return
	}

	rr = httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeHandler(w, r, mockSvc, testLogger, testTimeout)
	})

	handler.ServeHTTP(rr, req)

	return
}

// stalledLister answers nothing. It returns when the probe's context ends,
// as an endpoint that accepts the connection and then stalls does.
type stalledLister struct {
	started chan struct{}
	once    sync.Once
}

func newStalledLister() *stalledLister {
	return &stalledLister{started: make(chan struct{})}
}

func (s *stalledLister) ListObjectsV2(ctx context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// probeInBackground runs one probe against a stalled endpoint. It returns
// the lister, the recorder, and a channel that closes when the probe ends.
func probeInBackground(req *http.Request, timeout probeTimeout) (*stalledLister, *httptest.ResponseRecorder, chan struct{}) {
	svc := newStalledLister()
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		probeHandler(rr, req, svc, testLogger, timeout)
	}()
	return svc, rr, done
}

// wantProbeEnds fails the test if the probe does not end.
func wantProbeEnds(t *testing.T, done chan struct{}, rr *httptest.ResponseRecorder, prefix string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the probe did not end")
	}
	want := `s3_list_success{bucket="mock",delimiter="",prefix="` + prefix + `"} 0`
	if !strings.Contains(rr.Body.String(), want) {
		t.Errorf("want %s, got:\n%s", want, rr.Body.String())
	}
}

// A stalled endpoint must not hold the probe. The deadline ends the
// listing, and the probe reports s3_list_success 0.
func TestProbeEndsOnItsOwnDeadline(t *testing.T) {
	req, _ := http.NewRequest("GET", "/probe?target=mock/stalled", nil)
	_, rr, done := probeInBackground(req, probeTimeout{fallback: 200 * time.Millisecond})
	wantProbeEnds(t, done, rr, "stalled")
}

// Prometheus sends the scrape timeout in a header. The probe must use it,
// so it does not outlive the scrape and pay for pages nobody reads.
func TestProbeEndsOnTheScrapeTimeoutHeader(t *testing.T) {
	req, _ := http.NewRequest("GET", "/probe?target=mock/header", nil)
	req.Header.Set(scrapeTimeoutHeader, "0.2")
	_, rr, done := probeInBackground(req, probeTimeout{fallback: time.Hour})
	wantProbeEnds(t, done, rr, "header")
}

// Prometheus closes the connection when it gives up. The probe must stop
// there too.
func TestProbeEndsWhenTheScrapeEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequest("GET", "/probe?target=mock/cancelled", nil)
	svc, rr, done := probeInBackground(req.WithContext(ctx), probeTimeout{fallback: time.Hour})
	<-svc.started
	cancel()
	wantProbeEnds(t, done, rr, "cancelled")
}

func TestProbeTimeoutBudget(t *testing.T) {
	timeout := probeTimeout{fallback: 2 * time.Minute, offset: 500 * time.Millisecond}
	for _, c := range []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"no header", "", 2*time.Minute - 500*time.Millisecond},
		{"header", "30", 30*time.Second - 500*time.Millisecond},
		{"fractional header", "1.5", 1500*time.Millisecond - 500*time.Millisecond},
		{"header is not a number", "soon", 2*time.Minute - 500*time.Millisecond},
		{"header is 0", "0", 2*time.Minute - 500*time.Millisecond},
		{"header is smaller than the offset", "0.2", 200 * time.Millisecond},
	} {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/probe?target=mock/one", nil)
			if c.header != "" {
				req.Header.Set(scrapeTimeoutHeader, c.header)
			}
			if got := timeout.budget(req, testLogger); got != c.want {
				t.Errorf("want %s, got %s", c.want, got)
			}
		})
	}
}

// Functions to help return pointers succinctly
func String(s string) *string {
	return &s
}

func Time(t time.Time) *time.Time {
	return &t
}

func Int64(i int64) *int64 {
	return &i
}

func Int32(i int32) *int32 {
	return &i
}

func Bool(b bool) *bool {
	return &b
}
