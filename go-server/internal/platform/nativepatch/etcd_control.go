package nativepatch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const defaultEtcdNamespace = "longheng/nativepatch"

var (
	ErrEtcdControlInstanceID = errors.New("native patch etcd control instance id generation failed")
	ErrEtcdControlStore      = errors.New("native patch etcd control store is required")
	etcdControlRandRead      = rand.Read
)

type EtcdClient interface {
	Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Put(context.Context, string, string, ...clientv3.OpOption) (*clientv3.PutResponse, error)
	Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan
	Close() error
}

type EtcdControlOptions struct {
	Client      EtcdClient
	Namespace   string
	CloseClient bool
	Now         func() time.Time
}

type EtcdControlStore struct {
	client      EtcdClient
	namespace   string
	closeClient bool
	instanceID  string
	now         func() time.Time

	mu       sync.Mutex
	sequence uint64
}

func NewEtcdControlStore(options EtcdControlOptions) (*EtcdControlStore, error) {
	if options.Client == nil {
		return nil, ErrEtcdControlStore
	}
	namespace := normEtcdNamespace(options.Namespace)
	now := options.Now
	if now == nil {
		now = time.Now
	}
	instanceID, err := newEtcdInstID()
	if err != nil {
		return nil, err
	}
	return &EtcdControlStore{
		client:      options.Client,
		namespace:   namespace,
		closeClient: options.CloseClient,
		instanceID:  instanceID,
		now:         now,
	}, nil
}

func (s *EtcdControlStore) Publish(ctx context.Context, target string, intent ControlIntent) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	client, namespace, _, _, err := s.ready()
	if err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("native patch control target is required")
	}
	intent.Target = target
	if intent.Action == "" {
		intent.Action = ControlApply
	}
	if intent.Action != ControlApply && intent.Action != ControlRestore {
		return fmt.Errorf("unsupported native patch control action %s", intent.Action)
	}
	if strings.TrimSpace(intent.Version) == "" {
		return ErrVersionRequired
	}
	if intent.Action == ControlApply && strings.TrimSpace(intent.PackageDir) == "" {
		return ErrPlanRequired
	}
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	_, err = client.Put(ctx, intentKey(namespace, target), string(data))
	return err
}

func (s *EtcdControlStore) Watch(ctx context.Context, target string) (<-chan ControlIntent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, namespace, _, _, err := s.ready()
	if err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("native patch control target is required")
	}
	key := intentKey(namespace, target)
	out := make(chan ControlIntent, 1)
	resp, err := client.Get(ctx, key)
	if err != nil {
		close(out)
		return nil, err
	}
	if len(resp.Kvs) > 0 {
		if intent, err := decodeControlIntent(resp.Kvs[len(resp.Kvs)-1].Value); err == nil {
			if !sendControlIntent(ctx, out, intent) {
				return out, nil
			}
		}
	}
	watchCh := client.Watch(ctx, key)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case watchResp, ok := <-watchCh:
				if !ok {
					return
				}
				for _, event := range watchResp.Events {
					if event == nil || event.Kv == nil {
						continue
					}
					intent, err := decodeControlIntent(event.Kv.Value)
					if err != nil {
						continue
					}
					if !sendControlIntent(ctx, out, intent) {
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func sendControlIntent(ctx context.Context, out chan ControlIntent, intent ControlIntent) bool {
	select {
	case out <- intent:
		return true
	case <-ctx.Done():
		return false
	default:
	}
	select {
	case <-out:
	default:
	}
	select {
	case out <- intent:
		return true
	case <-ctx.Done():
		return false
	default:
		return true
	}
}

func (s *EtcdControlStore) Report(ctx context.Context, progress ControlProgress) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	client, namespace, now, instanceID, err := s.ready()
	if err != nil {
		return err
	}
	progress.Target = strings.TrimSpace(progress.Target)
	progress.NodeID = strings.TrimSpace(progress.NodeID)
	if progress.Target == "" || progress.NodeID == "" {
		return fmt.Errorf("native patch progress target and node_id are required")
	}
	if progress.UpdatedAt.IsZero() {
		progress.UpdatedAt = now().UTC()
	}
	data, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	_, err = client.Put(ctx, s.progressKey(namespace, instanceID, progress), string(data))
	return err
}

func (s *EtcdControlStore) ListProgress(ctx context.Context, target string) ([]ControlProgress, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, namespace, _, _, err := s.ready()
	if err != nil {
		return nil, err
	}
	resp, err := client.Get(ctx, progressPrefix(namespace, target), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	sort.Slice(resp.Kvs, func(i, j int) bool {
		return string(resp.Kvs[i].Key) < string(resp.Kvs[j].Key)
	})
	out := make([]ControlProgress, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var progress ControlProgress
		if err := json.Unmarshal(kv.Value, &progress); err != nil {
			return nil, err
		}
		out = append(out, progress)
	}
	return out, nil
}

func (s *EtcdControlStore) ready() (EtcdClient, string, func() time.Time, string, error) {
	if s == nil {
		return nil, "", nil, "", ErrEtcdControlStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, "", nil, "", ErrEtcdControlStore
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	instanceID := s.instanceID
	if instanceID == "" {
		generated, err := newEtcdInstID()
		if err != nil {
			return nil, "", nil, "", err
		}
		s.instanceID = generated
		instanceID = generated
	}
	return s.client, normEtcdNamespace(s.namespace), now, instanceID, nil
}

func (s *EtcdControlStore) Close() error {
	if s == nil || s.client == nil || !s.closeClient {
		return nil
	}
	return s.client.Close()
}

func normEtcdNamespace(namespace string) string {
	namespace = strings.Trim(strings.TrimSpace(namespace), "/")
	if namespace == "" {
		return defaultEtcdNamespace
	}
	return namespace
}

func intentKey(namespace string, target string) string {
	return namespace + "/intent/" + encodeEtcdPart(target)
}

func progressPrefix(namespace string, target string) string {
	return namespace + "/progress/" + encodeEtcdPart(target) + "/"
}

func (s *EtcdControlStore) progressKey(namespace string, instanceID string, progress ControlProgress) string {
	s.mu.Lock()
	s.sequence++
	sequence := s.sequence
	s.mu.Unlock()
	return fmt.Sprintf("%s%s/%020d/%s/%020d", progressPrefix(namespace, progress.Target), encodeEtcdPart(progress.NodeID), progress.UpdatedAt.UnixNano(), instanceID, sequence)
}

func newEtcdInstID() (string, error) {
	var b [8]byte
	if _, err := etcdControlRandRead(b[:]); err != nil {
		return "", fmt.Errorf("%w: %w", ErrEtcdControlInstanceID, err)
	}
	return hex.EncodeToString(b[:]), nil
}

func encodeEtcdPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeControlIntent(data []byte) (ControlIntent, error) {
	var intent ControlIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return ControlIntent{}, err
	}
	return intent, nil
}
