package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/Atrello/bastion/internal/protocol"
)

// Game backups kept in an S3 bucket rather than on this server. The panel sends where with each request
// (the keys aren't kept here). A backup is made here as usual, uploaded (in parts, so any size), and the
// copy here removed; restoring downloads it here first.

func s3Client(t *protocol.S3Target) (*minio.Client, error) {
	if t.Endpoint == "" || strings.ContainsAny(t.Endpoint, "/ ") || t.Bucket == "" || t.Key == "" || strings.Contains(t.Key, "..") {
		return nil, errors.New("the backup storage settings aren't complete")
	}
	lookup := minio.BucketLookupDNS
	if t.PathStyle {
		lookup = minio.BucketLookupPath
	}
	return minio.New(t.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(t.AccessKey, t.SecretKey, ""),
		Secure:       !t.PlainHTTP,
		Region:       t.Region,
		BucketLookup: lookup,
	})
}

// s3Problem puts what S3 said in plain words.
func s3Problem(doing string, err error) error {
	resp := minio.ToErrorResponse(err)
	switch resp.Code {
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return fmt.Errorf("%s: the storage refused the keys (%s)", doing, resp.Code)
	case "NoSuchBucket":
		return fmt.Errorf("%s: there's no such bucket", doing)
	case "NoSuchKey":
		return fmt.Errorf("%s: that backup isn't in the storage any more", doing)
	}
	return fmt.Errorf("%s: %w", doing, err)
}

func s3Upload(ctx context.Context, t *protocol.S3Target, file string) error {
	c, err := s3Client(t)
	if err != nil {
		return err
	}
	// The plainest way of sending, which every service that speaks S3 takes (not all take the streamed
	// signatures or trailing checksums some clients use), with an MD5 of each part so a damaged upload fails.
	_, err = c.FPutObject(ctx, t.Bucket, t.Key, file, minio.PutObjectOptions{
		ContentType: "application/gzip", DisableContentSha256: true, SendContentMd5: true,
	})
	if err != nil {
		return s3Problem("uploading the backup", err)
	}
	return nil
}

func s3Download(ctx context.Context, t *protocol.S3Target, file string) error {
	c, err := s3Client(t)
	if err != nil {
		return err
	}
	if err := c.FGetObject(ctx, t.Bucket, t.Key, file, minio.GetObjectOptions{}); err != nil {
		return s3Problem("fetching the backup", err)
	}
	return nil
}

func s3Delete(ctx context.Context, t *protocol.S3Target) error {
	c, err := s3Client(t)
	if err != nil {
		return err
	}
	if err := c.RemoveObject(ctx, t.Bucket, t.Key, minio.RemoveObjectOptions{}); err != nil {
		return s3Problem("deleting the backup", err)
	}
	return nil
}

// s3Piece reads up to chunkSize bytes of the backup from offset, and gives its whole size.
func s3Piece(ctx context.Context, t *protocol.S3Target, offset int64) ([]byte, int64, error) {
	c, err := s3Client(t)
	if err != nil {
		return nil, 0, err
	}
	info, err := c.StatObject(ctx, t.Bucket, t.Key, minio.StatObjectOptions{})
	if err != nil {
		return nil, 0, s3Problem("reading the backup", err)
	}
	if offset >= info.Size {
		return nil, info.Size, nil
	}
	opts := minio.GetObjectOptions{}
	_ = opts.SetRange(offset, min(offset+chunkSize, info.Size)-1)
	obj, err := c.GetObject(ctx, t.Bucket, t.Key, opts)
	if err != nil {
		return nil, 0, s3Problem("reading the backup", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(io.LimitReader(obj, chunkSize))
	if err != nil {
		return nil, 0, s3Problem("reading the backup", err)
	}
	return data, info.Size, nil
}

// withS3Copy fetches a backup kept in S3 to a file here, runs use on it, and removes the file.
func withS3Copy(ctx context.Context, t *protocol.S3Target, file string, use func(string) error) error {
	tmp := file + ".fetched"
	defer os.Remove(tmp)
	if err := s3Download(ctx, t, tmp); err != nil {
		return err
	}
	return use(tmp)
}
