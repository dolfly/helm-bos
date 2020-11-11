package bos

import (
	"net/url"

	"github.com/baidubce/bce-sdk-go/services/bos"
	"github.com/baidubce/bce-sdk-go/services/bos/api"
	"github.com/pkg/errors"
)

// NewClient creates a new bos client.
// Use Application Default Credentials if serviceAccount is empty.
func NewClient(ak, sk string) (*bos.Client, error) {
	client, err := bos.NewClient(ak, sk, "bj.bcebos.com")
	if err != nil {
		return nil, errors.Wrap(err, "new client")
	}
	return client, err
}

// Object retourne a new object handle for the given path
func Object(client *bos.Client, path string) (*api.GetObjectResult, error) {
	bucket, path, err := splitPath(path)
	if err != nil {
		return nil, errors.Wrap(err, "split path")
	}
	return client.BasicGetObject(bucket, path)
}

// UploadFile create object for the given path
func UploadFile(client *bos.Client, path string, fpath string) error {
	bucket, path, err := splitPath(path)
	if err != nil {
		return errors.Wrap(err, "split path")
	}
	_, err = client.PutObjectFromFile(bucket, path, fpath, nil)
	return err
}

// UploadByte create object for the given data
func UploadByte(client *bos.Client, path string, data []byte) error {
	bucket, path, err := splitPath(path)
	if err != nil {
		return errors.Wrap(err, "split path")
	}
	_, err = client.PutObjectFromBytes(bucket, path, data, &api.PutObjectArgs{
		CacheControl: "no-cache, max-age=0, no-transform",
	})
	return err
}

// Delete delete  object for the given path
func Delete(client *bos.Client, path string) error {
	bucket, path, err := splitPath(path)
	if err != nil {
		return errors.Wrap(err, "split path")
	}
	return client.DeleteObject(bucket, path)
}

func splitPath(gcsurl string) (bucket string, path string, err error) {
	u, err := url.Parse(gcsurl)
	if err != nil {
		return
	}
	if u.Scheme != "bs" && u.Scheme != "bos" {
		return "", "", errors.New(`incorrect url, should be "bos://bucket/path"`)
	}
	bucket = u.Host
	path = u.Path[1:]
	return
}
