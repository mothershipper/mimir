package bucketclient

import (
	"bytes"
	"context"
	"io/ioutil"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
)

const (
	// The bucket prefix under which all tenants alertmanager configs are stored.
	alertsPrefix = "alerts"
)

// BucketAlertStore is used to support the AlertStore interface against an object storage backend. It is implemented
// using the Thanos objstore.Bucket interface
type BucketAlertStore struct {
	bucket      objstore.Bucket
	cfgProvider bucket.TenantConfigProvider
	logger      log.Logger
}

func NewBucketAlertStore(bkt objstore.Bucket, cfgProvider bucket.TenantConfigProvider, logger log.Logger) *BucketAlertStore {
	return &BucketAlertStore{
		bucket:      bucket.NewPrefixedBucketClient(bkt, alertsPrefix),
		cfgProvider: cfgProvider,
		logger:      logger,
	}
}

// ListAlertConfigs implements alertstore.AlertStore.
func (s *BucketAlertStore) ListAlertConfigs(ctx context.Context) (map[string]alertspb.AlertConfigDesc, error) {
	cfgs := map[string]alertspb.AlertConfigDesc{}

	err := s.bucket.Iter(ctx, "", func(key string) error {
		userID := key

		cfg, err := s.getAlertConfig(ctx, userID)
		if err != nil {
			return errors.Wrapf(err, "failed to fetch alertmanager config for user %s", userID)
		}

		cfgs[cfg.User] = cfg
		return nil
	})

	if err != nil {
		return nil, err
	}

	return cfgs, nil
}

// GetAlertConfig implements alertstore.AlertStore.
func (s *BucketAlertStore) GetAlertConfig(ctx context.Context, userID string) (alertspb.AlertConfigDesc, error) {
	cfg, err := s.getAlertConfig(ctx, userID)
	if s.bucket.IsObjNotFoundErr(err) {
		return cfg, alertspb.ErrNotFound
	}

	return cfg, err
}

// SetAlertConfig implements alertstore.AlertStore.
func (s *BucketAlertStore) SetAlertConfig(ctx context.Context, cfg alertspb.AlertConfigDesc) error {
	cfgBytes, err := cfg.Marshal()
	if err != nil {
		return err
	}

	return s.bucket.Upload(ctx, cfg.User, bytes.NewBuffer(cfgBytes))
}

// DeleteAlertConfig implements alertstore.AlertStore.
func (s *BucketAlertStore) DeleteAlertConfig(ctx context.Context, userID string) error {
	err := s.bucket.Delete(ctx, userID)
	if s.bucket.IsObjNotFoundErr(err) {
		return nil
	}
	return err
}

func (s *BucketAlertStore) getAlertConfig(ctx context.Context, key string) (alertspb.AlertConfigDesc, error) {
	readCloser, err := s.bucket.Get(ctx, key)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	defer runutil.CloseWithLogOnErr(s.logger, readCloser, "close alertmanager config reader")

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	config := alertspb.AlertConfigDesc{}
	err = config.Unmarshal(buf)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	return config, nil
}