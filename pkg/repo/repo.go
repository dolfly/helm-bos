package repo

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	storage "github.com/baidubce/bce-sdk-go/services/bos"
	"github.com/dolfly/helm-bos/pkg/bos"
	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/helmpath"
	"helm.sh/helm/v3/pkg/provenance"
	"helm.sh/helm/v3/pkg/repo"
)

var (
	// ErrIndexOutOfDate occurs when trying to push a chart on a repository
	// that is being updated at the same time.
	ErrIndexOutOfDate = errors.New("index is out-of-date")

	// Debug is used to activate log output
	Debug bool
	log   = logger()
)

// Repo manages Helm repositories on Google Cloud Storage.
type Repo struct {
	entry               *repo.Entry
	indexFileURL        string
	indexFileGeneration int64
	bos                 *storage.Client
}

// New creates a new Repo object
func New(path string, bos *storage.Client) (*Repo, error) {
	indexFileURL, err := resolveReference(path, "index.yaml")
	if err != nil {
		return nil, errors.Wrap(err, "resolve index reference")
	}
	return &Repo{
		entry:        nil,
		indexFileURL: indexFileURL,
		bos:          bos,
	}, nil
}

// Load loads an existing repository known by Helm.
// Returns ErrNotFound if the repository is not found in helm repository entries.
func Load(name string, bos *storage.Client) (*Repo, error) {
	entry, err := retrieveRepositoryEntry(name)
	if err != nil {
		return nil, errors.Wrap(err, "repo entry")
	}

	indexFileURL, err := resolveReference(entry.URL, "index.yaml")
	if err != nil {
		return nil, errors.Wrap(err, "resolve index reference")
	}

	return &Repo{
		entry:        entry,
		indexFileURL: indexFileURL,
		bos:          bos,
	}, nil
}

// Create creates a new repository on BOS by uploading a blank index.yaml file.
// This function is idempotent.
func Create(r *Repo) error {
	log.Debugf("create a repository with index file at %s", r.indexFileURL)

	res, err := bos.Object(r.bos, r.indexFileURL)
	if err != nil {
		return errors.Wrap(err, "object")
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		i := repo.NewIndexFile()
		return r.uploadIndexFile(i)
	}
	defer res.Body.Close()
	if len(body) > 0 {
		log.Debugf("file %s already exists", r.indexFileURL)
		return nil
	}
	return err
}

// PushChart adds a chart into the repository.
//
// The index file on BOS will be updated and the file at "chartpath" will be uploaded to BOS.
// If the version of the chart is already indexed, it won't be uploaded unless "force" is set to true.
// The push will fail if the repository is updated at the same time, use "retry" to automatically reload
// the index of the repository.
func (r Repo) PushChart(chartpath string, force, retry bool, public bool, publicURL string) error {
	i, err := r.indexFile()
	if err != nil {
		return errors.Wrap(err, "load index file")
	}

	log.Debugf("load chart \"%s\" (force=%t, retry=%t, public=%t)", chartpath, force, retry, public)
	chart, err := loader.Load(chartpath)
	if err != nil {
		return errors.Wrap(err, "load chart")
	}

	log.Debugf("chart loaded: %s-%s", chart.Metadata.Name, chart.Metadata.Version)
	if i.Has(chart.Metadata.Name, chart.Metadata.Version) && !force {
		return fmt.Errorf("chart %s-%s already indexed. Use --force to still upload the chart", chart.Metadata.Name, chart.Metadata.Version)
	}

	err = r.updateIndexFile(i, chartpath, chart, public, publicURL)
	if err == ErrIndexOutOfDate && retry {
		for err == ErrIndexOutOfDate {
			i, err = r.indexFile()
			if err != nil {
				return errors.Wrap(err, "load index file")
			}
			err = r.updateIndexFile(i, chartpath, chart, public, publicURL)
		}
	}
	if err != nil {
		return errors.Wrap(err, "update index file")
	}

	log.Debugf("upload file to GCS")
	err = r.uploadChart(chartpath)
	if err != nil {
		return errors.Wrap(err, "write chart")
	}
	return nil
}

// RemoveChart removes a chart from the repository
// If version is empty, all version will be deleted.
func (r Repo) RemoveChart(name, version string, retry bool) error {
	log.Debugf("removing chart %s-%s", name, version)

removeChart:
	index, err := r.indexFile()
	if err != nil {
		return errors.Wrap(err, "index")
	}

	vs, ok := index.Entries[name]
	if !ok {
		return fmt.Errorf("chart \"%s\" not found", name)
	}

	urls := []string{}
	for i, v := range vs {
		if version == "" || version == v.Version {
			log.Debugf("%s-%s will be deleted", name, v.Version)
			chartURL := fmt.Sprintf("%s/%s-%s.tgz", r.entry.URL, name, v.Version)
			urls = append(urls, chartURL)
		}
		if version == v.Version {
			vs[i] = vs[len(vs)-1]
			vs[len(vs)-1] = nil
			index.Entries[name] = vs[:len(vs)-1]
			break
		}
	}
	if version == "" || len(index.Entries[name]) == 0 {
		delete(index.Entries, name)
	}

	err = r.uploadIndexFile(index)
	if err == ErrIndexOutOfDate && retry {
		goto removeChart
	}

	if err != nil {
		return err
	}

	// Delete charts from BOS
	for _, url := range urls {
		log.Debugf("delete bos file %s", url)
		err = bos.Delete(r.bos, url)
		if err != nil {
			return errors.Wrap(err, "delete")
		}
	}
	return nil
}

// uploadIndexFile update the index file on BOS.
func (r Repo) uploadIndexFile(i *repo.IndexFile) error {
	log.Debugf("push index file")
	i.SortEntries()
	i.Generated = time.Now()
	b, err := yaml.Marshal(i)
	if err != nil {
		return errors.Wrap(err, "marshal")
	}
	return bos.UploadByte(r.bos, r.indexFileURL, b)
}

// indexFile retrieves the index file from GCS.
// It will also retrieve the generation number of the file, for optimistic locking.
func (r *Repo) indexFile() (*repo.IndexFile, error) {
	log.Debugf("load index file \"%s\"", r.indexFileURL)

	// retrieve index file generation
	res, err := bos.Object(r.bos, r.indexFileURL)
	if err != nil {
		return nil, errors.Wrap(err, "object")
	}
	//r.indexFileGeneration = res.ObjectMeta.LastModified
	log.Debugf("index file generation: %d", r.indexFileGeneration)
	// get file
	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read")
	}
	defer res.Body.Close()
	i := &repo.IndexFile{}
	if err := yaml.Unmarshal(b, i); err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}
	i.SortEntries()
	return i, nil
}

// uploadChart pushes a chart into the repository.
func (r Repo) uploadChart(chartpath string) error {
	_, fname := filepath.Split(chartpath)
	chartURL, err := resolveReference(r.entry.URL, fname)
	if err != nil {
		return errors.Wrap(err, "resolve reference")
	}
	log.Debugf("upload file %s to bos path %s", fname, chartURL)
	return bos.UploadFile(r.bos, chartURL, chartpath)
}

func (r Repo) updateIndexFile(i *repo.IndexFile, chartpath string, chart *chart.Chart, public bool, publicURL string) error {
	hash, err := provenance.DigestFile(chartpath)
	if err != nil {
		return errors.Wrap(err, "generate chart file digest")
	}
	url, err := getURL(r.entry.URL, public, publicURL)
	if err != nil {
		return errors.Wrap(err, "get chart base url")
	}
	_, fname := filepath.Split(chartpath)
	log.Debugf("indexing chart '%s-%s' as '%s' (base url: %s)", chart.Metadata.Name, chart.Metadata.Version, fname, url)
	// Need to remove current version of chart if there is any
	currentChart, _ := i.Get(chart.Metadata.Name, chart.Metadata.Version)
	if currentChart != nil {
		chartVersions := i.Entries[chart.Metadata.Name]
		for idx, ver := range chartVersions {
			if ver.Version == currentChart.Version {
				chartVersions[idx] = chartVersions[len(chartVersions)-1]
				chartVersions[len(chartVersions)-1] = nil
				i.Entries[chart.Metadata.Name] = chartVersions[:len(chartVersions)-1]
				break
			}
		}
	}
	i.Add(chart.Metadata, fname, url, hash)
	return r.uploadIndexFile(i)
}

func getURL(base string, public bool, publicURL string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if public && publicURL != "" {
		return publicURL, nil
	} else if public {
		return fmt.Sprintf("https://%s.cdn.bcebos.com/%s", baseURL.Host, baseURL.Path), nil
	}
	return baseURL.String(), nil
}

func resolveReference(base, p string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", errors.Wrap(err, "url parsing")
	}
	baseURL.Path = path.Join(baseURL.Path, p)
	return baseURL.String(), nil
}

func retrieveRepositoryEntry(name string) (*repo.Entry, error) {
	repoFilePath := envOr("HELM_REPOSITORY_CONFIG", helmpath.ConfigPath("repositories.yaml"))
	log.Debugf("helm repo file: %s", repoFilePath)

	repoFile, err := repo.LoadFile(repoFilePath)
	if err != nil {
		return nil, errors.Wrap(err, "load repo file")
	}

	for _, r := range repoFile.Repositories {
		if r.Name == name {
			return r, nil
		}
	}

	return nil, fmt.Errorf("repository \"%s\" does not exist", name)
}

func logger() *logrus.Entry {
	l := logrus.New()
	level := logrus.InfoLevel
	if Debug || strings.ToLower(os.Getenv("HELM_BOS_DEBUG")) == "true" {
		level = logrus.DebugLevel
	}
	l.SetLevel(level)
	l.Formatter = &logrus.TextFormatter{}
	return logrus.NewEntry(l)
}

func envOr(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return def
}
