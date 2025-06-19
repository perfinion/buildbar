package elasticsearch

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"time"

	// "github.com/aws/aws-sdk-go-v2/aws"
	// "github.com/aws/aws-sdk-go-v2/config"
	opensearch "github.com/opensearch-project/opensearch-go/v4"
	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	// requestsigner "github.com/opensearch-project/opensearch-go/v4/signer/awsv2"

	"github.com/buildbarn/bb-storage/pkg/util"
	pb "github.com/meroton/buildbar/proto/configuration/elasticsearch"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewClientFromConfiguration creates an elasticsearch.TypedClient object
// based on the provided configuration.
func NewClientFromConfiguration(configuration *pb.ClientConfiguration) (*opensearchapi.Client, error) {
	if configuration == nil {
		return nil, status.Error(codes.InvalidArgument, "No Elasticsearch client configuration provided")
	}

	// awsCfg, err := config.LoadDefaultConfig(ctx,
	// 	config.WithRegion("<AWS_REGION>"),
	// 	config.WithCredentialsProvider(
	// 		getCredentialProvider("<AWS_ACCESS_KEY>", "<AWS_SECRET_ACCESS_KEY>", "<AWS_SESSION_TOKEN>"),
	// 	),
	// )
	// if err != nil {
	// 	log.Fatal(err) // Do not log.fatal in a production ready app.
	// }

	elasticsearchConfig := opensearch.Config{
		Addresses: configuration.Addresses,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: configuration.SkipVerifyTls},
		},
	}
	// Client: opensearch.Config{
	// Transport: &http.Transport{
	// TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // For testing only. Use certificate for validation.
	// },
	// Addresses: []string{"https://localhost:9200"},
	// Username:  "admin", // For testing only. Don't store credentials in code.
	// Password:  "admin",
	// },

	switch credentials := configuration.Credentials.Credentials.(type) {
	case *pb.ClientCredentials_BasicAuth:
		elasticsearchConfig.Username = credentials.BasicAuth.Username
		elasticsearchConfig.Password = credentials.BasicAuth.Password
	// case *pb.ClientCredentials_ApiKey:
	// 	elasticsearchConfig.APIKey = credentials.ApiKey
	// case *pb.ClientCredentials_ServiceToken:
	// 	elasticsearchConfig.ServiceToken = credentials.ServiceToken
	default:
		return nil, status.Error(codes.InvalidArgument,
			"Configuration did not contain a supported Elasticsearch credentials type")
	}

	client, err := opensearchapi.NewClient(
		opensearchapi.Config{
			Client: elasticsearchConfig,
		},
	)
	if err != nil {
		return nil, util.StatusWrap(err, "Failed to create Elasticsearch client")
	}
	return client, nil
}

// DieWhenConnectionFails checks periodically that Elasticsearch can be
// reached. If this fails after a few retries, it panics.
//
// TODO: Patch bb-storage to allow for dynamic health status.
func DieWhenConnectionFails(ctx context.Context, es *opensearchapi.Client) {
	retries := -1
	lastSuccess := time.Now()
	for {
		res, err := es.Info(ctx, nil)
		if err != nil {
			log.Printf("Failed to get Elasticsearch server info: %s", err)
			retries++
			downDuration := time.Since(lastSuccess)
			if retries > 5 && downDuration.Seconds() > 60 {
				log.Fatalf("Elasticsearch connection failed for %s with %d retries", downDuration, retries)
			}
		} else {
			if retries != 0 {
				log.Printf("Elasticsearch info: %#v\n", res)
				retries = 0
				lastSuccess = time.Now()
			}
		}
		time.Sleep(10 * time.Second)
	}
}
