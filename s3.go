package main

import (
	"log"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3 struct {
	session *session.Session
	Config  *S3Config
	DryRun  bool
}

func (s3 *S3) Connect() (err error) {
	s3.session, err = session.NewSession(
		&aws.Config{
			Credentials:      credentials.NewStaticCredentials(s3.Config.AccessKey, s3.Config.SecretKey, ""),
			Region:           aws.String(s3.Config.Region),
			Endpoint:         aws.String(s3.Config.Endpoint),
			DisableSSL:       aws.Bool(s3.Config.DisableSSL),
			S3ForcePathStyle: aws.Bool(s3.Config.ForcePathStyle),
		},
	)
	return
}

func (s3 *S3) Upload(localPath string, dstPath string) error {
	uploader := s3manager.NewUploader(s3.session)
	iter, err := s3.newSyncFolderIterator(localPath, dstPath)
	if err != nil {
		return err
	}
	log.Printf("Ready for upload %d files", len(iter.fileInfos))
	if s3.DryRun {
		log.Printf("... skip because dry-dun")
		return nil
	}
	if err := uploader.UploadWithIterator(aws.BackgroundContext(), iter); err != nil {
		return err
	}

	return iter.Err()
}

func (s3 *S3) Download(s3Path string, localPath string) error {
	return nil
}

// SyncFolderIterator is used to upload a given folder
// to Amazon S3.
type SyncFolderIterator struct {
	bucket    string
	fileInfos []fileInfo
	err       error
	acl       string
	s3path    string
}

type fileInfo struct {
	key      string
	fullpath string
}

func (s3 *S3) newSyncFolderIterator(localPath, dstPath string) (*SyncFolderIterator, error) {
	metadata := []fileInfo{}
	err := filepath.Walk(localPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			key := strings.TrimPrefix(filePath, localPath)
			metadata = append(metadata, fileInfo{key, filePath})
		}
		return nil
	})

	return &SyncFolderIterator{
		bucket:    s3.Config.Bucket,
		fileInfos: metadata,
		acl:       s3.Config.ACL,
		s3path:    path.Join(s3.Config.Path, dstPath),
	}, err
}

// Next will determine whether or not there is any remaining files to
// be uploaded.
func (iter *SyncFolderIterator) Next() bool {
	return len(iter.fileInfos) > 0
}

// Err returns any error when os.Open is called.
func (iter *SyncFolderIterator) Err() error {
	return iter.err
}

// UploadObject will prep the new upload object by open that file and constructing a new
// s3manager.UploadInput.
func (iter *SyncFolderIterator) UploadObject() s3manager.BatchUploadObject {
	fi := iter.fileInfos[0]
	iter.fileInfos = iter.fileInfos[1:]
	body, err := os.Open(fi.fullpath)
	if err != nil {
		iter.err = err
	}

	extension := filepath.Ext(fi.key)
	mimeType := mime.TypeByExtension(extension)

	if mimeType == "" {
		mimeType = "binary/octet-stream"
	}
	key := path.Join(iter.s3path, fi.key)
	input := s3manager.UploadInput{
		ACL:         &iter.acl,
		Bucket:      &iter.bucket,
		Key:         &key,
		Body:        body,
		ContentType: &mimeType,
	}

	return s3manager.BatchUploadObject{
		&input,
		nil,
	}
}