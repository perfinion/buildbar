package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	opensearch "github.com/opensearch-project/opensearch-go/v4"
	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"

	"github.com/buildbarn/bb-storage/pkg/clock"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
)

var (
	uploaderPrometheusMetrics sync.Once

	uploaderUploadDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "buildbar",
			Subsystem: "elasticsearch",
			Name:      "upload_duration_seconds",
			Help:      "Amount of time spent from receiving data until it was stored in the database.",
			Buckets:   util.DecimalExponentialBuckets(-3, 6, 2),
		},
		[]string{"index", "result"})
)

// The Uploader can be used to push documents into Elasticsearch.
type Uploader interface {
	Put(ctx context.Context, id string, document interface{}) error
}

type uploader struct {
	elasticsearchClient *opensearchapi.Client
	index               string
	clock               clock.Clock
	warningLogger       util.ErrorLogger
}

// NewUploader creates a new Uploader that uploads generic documents to Elasticsearch.
func NewUploader(
	elasticsearchClient *opensearchapi.Client,
	index string,
	clock clock.Clock,
	warningLogger util.ErrorLogger,
) Uploader {
	uploaderPrometheusMetrics.Do(func() {
		prometheus.MustRegister(uploaderUploadDurationSeconds)
	})

	return &uploader{
		elasticsearchClient: elasticsearchClient,
		index:               index,
		clock:               clock,
		warningLogger:       warningLogger,
	}
}

// Put uploads a document with a specific id to Elasticsearch.
// The assumption is that documents are only uploaded once.
// If a document with the same id exists, the document will be replaced
// and a log line will be written.
func (u *uploader) Put(ctx context.Context, id string, document interface{}) error {
	indexStartTime := u.clock.Now()
	var statusCode codes.Code
	jsonBody, err := json.Marshal(document)
	if err != nil {
		// There is no point to retry if the error code is under 500.
		duration := u.clock.Now().Sub(indexStartTime)
		uploaderUploadDurationSeconds.
			WithLabelValues(u.index, "client-error").
			Observe(duration.Seconds())
		statusCode = codes.InvalidArgument
		err = util.StatusWrapfWithCode(err, statusCode, "Failed to index document %s into %s in Elasticsearch", id, u.index)
		u.warningLogger.Log(err)
		return err
	}

	res, err := u.elasticsearchClient.Index(ctx, opensearchapi.IndexReq{
		Index:      u.index,
		DocumentID: id,
		Body:       bytes.NewReader(jsonBody),
	})
	duration := u.clock.Now().Sub(indexStartTime)
	if err != nil {
		var esErr *opensearch.StringError
		var lblVal string

		if errors.As(err, &esErr) && esErr.Status < 500 {
			// There is no point to retry if the error code is under 500.
			statusCode, lblVal = codes.InvalidArgument, "client-error"
		} else {
			// Retry >=500 errors.
			statusCode, lblVal = codes.Unknown, "transport-error"
		}

		uploaderUploadDurationSeconds.
			WithLabelValues(u.index, lblVal).
			Observe(duration.Seconds())
		err = util.StatusWrapfWithCode(err, statusCode, "Failed to index document %s into %s in Elasticsearch", id, u.index)
		u.warningLogger.Log(err)
		return err
	}
	uploaderUploadDurationSeconds.
		WithLabelValues(u.index, fmt.Sprintf("%v", res.Inspect().Response.StatusCode)).
		Observe(duration.Seconds())
	if res.Result != "created" {
		u.warningLogger.Log(fmt.Errorf(
			"Unexpected successful result when indexing %s into %s in Elasticsearch: %v", id, u.index, res.Result,
		))
	}
	return nil
}
