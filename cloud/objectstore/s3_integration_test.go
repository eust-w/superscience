package objectstore_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"superscience/cloud"
	objectstore "superscience/cloud/objectstore"
)

func TestS3ObjectStoreLifecycle(t *testing.T) {
	endpoint := os.Getenv("SUPERSCIENCE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("SUPERSCIENCE_TEST_S3_ENDPOINT is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	objects, err := objectstore.NewS3(ctx, objectstore.S3Config{
		Endpoint: endpoint, Region: os.Getenv("SUPERSCIENCE_TEST_S3_REGION"),
		Bucket: os.Getenv("SUPERSCIENCE_TEST_S3_BUCKET"), AccessKey: os.Getenv("SUPERSCIENCE_TEST_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("SUPERSCIENCE_TEST_S3_SECRET_KEY"), EnsureBucket: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ cloud.ObjectStore = objects
	if err := objects.Health(ctx); err != nil {
		t.Fatal(err)
	}
	key := "integration/" + time.Now().Format("20060102T150405.000000000") + "/artifact.txt"
	content := "Superscience S3 evidence"
	info, err := objects.Put(ctx, key, "text/plain; charset=utf-8", strings.NewReader(content), int64(len(content)))
	if err != nil || info.Key != key || info.Size != int64(len(content)) || info.ETag == "" {
		t.Fatalf("put info=%+v err=%v", info, err)
	}
	loaded, err := objects.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(loaded.Body)
	closeErr := loaded.Body.Close()
	if err != nil || closeErr != nil || string(payload) != content || loaded.Size != int64(len(content)) {
		t.Fatalf("get payload=%q info=%+v err=%v close=%v", payload, loaded, err, closeErr)
	}
	if err := objects.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Get(ctx, key); err == nil {
		t.Fatal("deleted object remained readable")
	}
}

func TestS3ObjectStoreRejectsInsecureNonLoopbackEndpoint(t *testing.T) {
	_, err := objectstore.NewS3(context.Background(), objectstore.S3Config{
		Endpoint: "http://objects.example.com", Bucket: "superscience", AccessKey: "access", SecretKey: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure S3 endpoint error = %v", err)
	}
}
