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

// s3_exporter answers "what is in this bucket under this prefix" as
// Prometheus metrics, one ListObjects per probe: how many objects, how
// big, and — the reason SFI forked it — when the newest one was written.
//
// Probe model, like blackbox_exporter: GET /probe?bucket=B&prefix=P, or
// /probe?target=B/P for the prometheus-operator Probe CR, which passes a
// single `target`. No state, no cache; every scrape is one listing.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
)

const namespace = "s3"

var (
	s3ListSuccess = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "list_success"),
		"If the ListObjects operation was a success",
		[]string{"bucket", "prefix", "delimiter"}, nil,
	)
	s3ListDuration = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "list_duration_seconds"),
		"The total duration of the list operation",
		[]string{"bucket", "prefix", "delimiter"}, nil,
	)
	s3LastModifiedObjectDate = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "last_modified_object_date"),
		"The last modified date of the object that was modified most recently",
		[]string{"bucket", "prefix"}, nil,
	)
	s3LastModifiedObjectSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "last_modified_object_size_bytes"),
		"The size of the object that was modified most recently",
		[]string{"bucket", "prefix"}, nil,
	)
	s3ObjectTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "objects"),
		"The total number of objects for the bucket/prefix combination",
		[]string{"bucket", "prefix"}, nil,
	)
	s3SumSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "objects_size_sum_bytes"),
		"The total size of all objects summed",
		[]string{"bucket", "prefix"}, nil,
	)
	s3BiggestSize = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "biggest_object_size_bytes"),
		"The size of the biggest object",
		[]string{"bucket", "prefix"}, nil,
	)
	s3CommonPrefixes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "common_prefixes"),
		"A count of all the keys between the prefix and the next occurrence of the string specified by the delimiter",
		[]string{"bucket", "prefix", "delimiter"}, nil,
	)
)

// lister is the one S3 call this exporter makes; tests supply a fake.
type lister interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// Exporter collects one bucket/prefix (optionally by delimiter).
type Exporter struct {
	bucket    string
	prefix    string
	delimiter string
	svc       lister
	logger    *slog.Logger
}

// Describe all the metrics we export
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- s3ListSuccess
	ch <- s3ListDuration
	if e.delimiter == "" {
		ch <- s3LastModifiedObjectDate
		ch <- s3LastModifiedObjectSize
		ch <- s3ObjectTotal
		ch <- s3SumSize
		ch <- s3BiggestSize
	} else {
		ch <- s3CommonPrefixes
	}
}

// Collect metrics
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	var lastModified time.Time
	var numberOfObjects float64
	var totalSize int64
	var biggestObjectSize int64
	var lastObjectSize int64
	var commonPrefixes int

	query := &s3.ListObjectsV2Input{
		Bucket: aws.String(e.bucket),
		Prefix: aws.String(e.prefix),
	}
	if e.delimiter != "" {
		query.Delimiter = aws.String(e.delimiter)
	}

	// Continue making requests until we've listed and compared the date of every object
	startList := time.Now()
	for {
		resp, err := e.svc.ListObjectsV2(context.Background(), query)
		if err != nil {
			e.logger.Error("list failed", "bucket", e.bucket, "prefix", e.prefix, "err", err)
			// Upstream emitted this with two label values for a three-label
			// metric, which panics the collector — so a refused listing
			// (a wrong key, an exceeded cap) produced no s3_list_success 0,
			// the one sample that case exists to produce.
			ch <- prometheus.MustNewConstMetric(
				s3ListSuccess, prometheus.GaugeValue, 0, e.bucket, e.prefix, e.delimiter,
			)
			return
		}
		commonPrefixes += len(resp.CommonPrefixes)
		for _, item := range resp.Contents {
			numberOfObjects++
			size := aws.ToInt64(item.Size)
			totalSize += size
			if item.LastModified != nil && item.LastModified.After(lastModified) {
				lastModified = *item.LastModified
				lastObjectSize = size
			}
			if size > biggestObjectSize {
				biggestObjectSize = size
			}
		}
		if resp.NextContinuationToken == nil {
			break
		}
		query.ContinuationToken = resp.NextContinuationToken
	}
	listDuration := time.Since(startList).Seconds()

	ch <- prometheus.MustNewConstMetric(
		s3ListSuccess, prometheus.GaugeValue, 1, e.bucket, e.prefix, e.delimiter,
	)
	ch <- prometheus.MustNewConstMetric(
		s3ListDuration, prometheus.GaugeValue, listDuration, e.bucket, e.prefix, e.delimiter,
	)
	if e.delimiter == "" {
		ch <- prometheus.MustNewConstMetric(
			s3LastModifiedObjectDate, prometheus.GaugeValue, float64(lastModified.UnixNano()/1e9), e.bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3LastModifiedObjectSize, prometheus.GaugeValue, float64(lastObjectSize), e.bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3ObjectTotal, prometheus.GaugeValue, numberOfObjects, e.bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3BiggestSize, prometheus.GaugeValue, float64(biggestObjectSize), e.bucket, e.prefix,
		)
		ch <- prometheus.MustNewConstMetric(
			s3SumSize, prometheus.GaugeValue, float64(totalSize), e.bucket, e.prefix,
		)
	} else {
		ch <- prometheus.MustNewConstMetric(
			s3CommonPrefixes, prometheus.GaugeValue, float64(commonPrefixes), e.bucket, e.prefix, e.delimiter,
		)
	}
}

// probeTarget resolves the request's bucket and prefix. `bucket` (+
// `prefix`) is upstream's form; `target=bucket/prefix` is the one a
// prometheus-operator Probe can express, since it passes one parameter.
func probeTarget(r *http.Request) (bucket, prefix string, err error) {
	q := r.URL.Query()
	bucket, prefix = q.Get("bucket"), q.Get("prefix")
	if target := q.Get("target"); target != "" {
		if bucket != "" {
			return "", "", errors.New("give either target or bucket, not both")
		}
		bucket, prefix, _ = strings.Cut(target, "/")
	}
	if bucket == "" {
		return "", "", errors.New("bucket parameter is missing (bucket=B&prefix=P, or target=B/P)")
	}
	return bucket, prefix, nil
}

func probeHandler(w http.ResponseWriter, r *http.Request, svc lister, logger *slog.Logger) {
	bucket, prefix, err := probeTarget(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	exporter := &Exporter{
		bucket:    bucket,
		prefix:    prefix,
		delimiter: r.URL.Query().Get("delimiter"),
		svc:       svc,
		logger:    logger,
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(exporter)

	// Serve
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

type discoveryTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// discoveryHandler is upstream's http_sd: one target per bucket the
// credential can list. Kept for parity; the SFI deployment lists
// prefixes it knows from the manifests instead.
func discoveryHandler(w http.ResponseWriter, r *http.Request, svc *s3.Client, logger *slog.Logger) {
	result, err := svc.ListBuckets(r.Context(), &s3.ListBucketsInput{})
	if err != nil {
		logger.Error("list buckets failed", "err", err)
		http.Error(w, "error listing buckets", http.StatusInternalServerError)
		return
	}

	targets := []discoveryTarget{}
	for _, b := range result.Buckets {
		name := aws.ToString(b.Name)
		targets = append(targets, discoveryTarget{
			Targets: []string{r.Host},
			Labels: map[string]string{
				"__param_bucket": name,
				"bucket":         name,
			},
		})
	}

	data, err := json.Marshal(targets)
	if err != nil {
		http.Error(w, "error marshalling targets", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func main() {
	var (
		app            = kingpin.New(namespace+"_exporter", "Export metrics for S3 buckets and objects").DefaultEnvars()
		listenAddress  = app.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9340").String()
		metricsPath    = app.Flag("web.metrics-path", "Path under which to expose metrics").Default("/metrics").String()
		probePath      = app.Flag("web.probe-path", "Path under which to expose the probe endpoint").Default("/probe").String()
		discoveryPath  = app.Flag("web.discovery-path", "Path under which to expose service discovery").Default("/discovery").String()
		endpointURL    = app.Flag("s3.endpoint-url", "Custom endpoint URL").Default("").String()
		forcePathStyle = app.Flag("s3.force-path-style", "Custom force path style").Bool()
		region         = app.Flag("s3.region", "Region (also AWS_REGION); some S3-compatible stores want one even when the endpoint decides it").Default("").String()
	)

	app.Version(version.Print(namespace + "_exporter"))
	app.HelpFlag.Short('h')
	kingpin.MustParse(app.Parse(os.Args[1:]))

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// Credentials from the environment (AWS_ACCESS_KEY_ID /
	// AWS_SECRET_ACCESS_KEY) or the usual SDK chain.
	var loadOpts []func(*config.LoadOptions) error
	if *region != "" {
		loadOpts = append(loadOpts, config.WithRegion(*region))
	}
	cfg, err := config.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		logger.Error("loading AWS config", "err", err)
		os.Exit(1)
	}
	svc := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if *endpointURL != "" {
			o.BaseEndpoint = aws.String(*endpointURL)
		}
		o.UsePathStyle = *forcePathStyle
	})

	logger.Info("starting "+namespace+"_exporter", "version", version.Info(), "build", version.BuildContext())

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc(*probePath, func(w http.ResponseWriter, r *http.Request) {
		probeHandler(w, r, svc, logger)
	})
	http.HandleFunc(*discoveryPath, func(w http.ResponseWriter, r *http.Request) {
		discoveryHandler(w, r, svc, logger)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
						 <head><title>S3 Exporter</title></head>
						 <body>
						 <h1>S3 Exporter</h1>
						 <p><a href="` + *probePath + `?bucket=BUCKET&prefix=PREFIX">Query metrics for objects in BUCKET that match PREFIX</a> (or ?target=BUCKET/PREFIX)</p>
						 <p><a href='` + *metricsPath + `'>Metrics</a></p>
						 <p><a href='` + *discoveryPath + `'>Service Discovery</a></p>
						 </body>
						 </html>`))
	})

	logger.Info("listening", "address", *listenAddress)
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
}
