// This file is part of MinIO Direct CSI
// Copyright (c) 2021 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package listener

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	// objectstorage
	v1beta1 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta1"
	"github.com/minio/direct-csi/pkg/clientset"
	"github.com/minio/direct-csi/pkg/utils"

	// k8s api
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	// k8s client
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	// logging
	"k8s.io/klog"
)

type addFunc func(ctx context.Context, obj interface{}) error
type updateFunc func(ctx context.Context, old, new interface{}) error
type deleteFunc func(ctx context.Context, obj interface{}) error

type addOp struct {
	Object  interface{}
	AddFunc *addFunc

	Key string
}

func (a addOp) String() string {
	return a.Key
}

type updateOp struct {
	OldObject  interface{}
	NewObject  interface{}
	UpdateFunc *updateFunc

	Key string
}

func (u updateOp) String() string {
	return u.Key
}

type deleteOp struct {
	Object     interface{}
	DeleteFunc *deleteFunc

	Key string
}

func (d deleteOp) String() string {
	return d.Key
}

type DirectCSIController struct {
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration

	// Controller
	ResyncPeriod time.Duration
	queue        workqueue.RateLimitingInterface
	threadiness  int

	// Listeners
	DirectCSIVolumeListener DirectCSIVolumeListener
	DirectCSIDriveListener  DirectCSIDriveListener

	// leader election
	leaderLock string
	identity   string

	// internal
	initialized     bool
	directcsiClient clientset.Interface
	kubeClient      kubeclientset.Interface

	locker     map[string]*sync.Mutex
	lockerLock sync.Mutex
}

func NewDefaultDirectCSIController(identity string, leaderLockName string, threads int) (*DirectCSIController, error) {
	rateLimit := workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(100*time.Millisecond, 600*time.Second),
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)
	return NewDirectCSIController(identity, leaderLockName, threads, rateLimit)
}

func NewDirectCSIController(identity string, leaderLockName string, threads int, limiter workqueue.RateLimiter) (*DirectCSIController, error) {
	var err error
	directcsiClient := utils.GetDirectClientset()
	kubeClient := utils.GetKubeClient()

	id := identity
	if id == "" {
		id, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}

	return &DirectCSIController{
		identity:        id,
		kubeClient:      kubeClient,
		directcsiClient: directcsiClient,
		initialized:     false,
		leaderLock:      leaderLockName,
		queue:           workqueue.NewRateLimitingQueue(limiter),
		threadiness:     threads,

		ResyncPeriod:  60 * time.Second,
		LeaseDuration: 60 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   5 * time.Second,
	}, nil
}

// Run - runs the controller. Note that ctx must be cancellable i.e. ctx.Done() should not return nil
func (c *DirectCSIController) Run(ctx context.Context) error {
	if !c.initialized {
		fmt.Errorf("Uninitialized controller. Atleast 1 listener should be added")
	}

	ns := func() string {
		if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
			return ns
		}

		if data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
				return ns
			}
		}
		return "default"
	}()

	sanitize := func(n string) string {
		re := regexp.MustCompile("[^a-zA-Z0-9-]")
		name := strings.ToLower(re.ReplaceAllString(n, "-"))
		if name[len(name)-1] == '-' {
			// name must not end with '-'
			name = name + "X"
		}
		return name
	}

	leader := sanitize(fmt.Sprintf("%s/%s", c.leaderLock, c.identity))
	id, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("error getting the default leader identity: %v", err)
	}

	recorder := record.NewBroadcaster()
	recorder.StartRecordingToSink(&corev1.EventSinkImpl{Interface: c.kubeClient.CoreV1().Events(ns)})
	eRecorder := recorder.NewRecorder(scheme.Scheme, v1.EventSource{Component: leader})

	rlConfig := resourcelock.ResourceLockConfig{
		Identity:      sanitize(id),
		EventRecorder: eRecorder,
	}

	l, err := resourcelock.New(resourcelock.LeasesResourceLock, ns, leader, c.kubeClient.CoreV1(), c.kubeClient.CoordinationV1(), rlConfig)
	if err != nil {
		return err
	}

	leaderConfig := leaderelection.LeaderElectionConfig{
		Lock:          l,
		LeaseDuration: c.LeaseDuration,
		RenewDeadline: c.RenewDeadline,
		RetryPeriod:   c.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				klog.V(2).Info("became leader, starting")
				c.runController(ctx)
			},
			OnStoppedLeading: func() {
				klog.Fatal("stopped leading")
			},
			OnNewLeader: func(identity string) {
				klog.V(3).Infof("new leader detected, current leader: %s", identity)
			},
		},
	}

	leaderelection.RunOrDie(ctx, leaderConfig)
	return nil // should never reach here
}

func (c *DirectCSIController) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *DirectCSIController) processNextItem(ctx context.Context) bool {
	// Wait until there is a new item in the working queue
	op, quit := c.queue.Get()
	if quit {
		return false
	}

	// With the lock below in place, we can safely tell the queue that we are done
	// processing this item. The lock will ensure that multiple items of the same
	// name and kind do not get processed simultaneously
	defer c.queue.Done(op)

	// Ensure that multiple operations on different versions of the same object
	// do not happen in parallel
	c.OpLock(op)
	defer c.OpUnlock(op)

	var opKind string
	var key string
	var err error

	switch o := op.(type) {
	case addOp:
		opKind = "add"
		key = o.Key
		add := *o.AddFunc
		err = add(ctx, o.Object)
	case updateOp:
		opKind = "update"
		key = o.Key
		update := *o.UpdateFunc
		err = update(ctx, o.OldObject, o.NewObject)
	case deleteOp:
		opKind = "delete"
		key = o.Key
		delete := *o.DeleteFunc
		err = delete(ctx, o.Object)
	default:
		panic("unknown item in queue")
	}
	if err != nil {
		klog.Errorf("op: %s key: %s err: %v", opKind, key, err)
	}
	return true
}

func (c *DirectCSIController) OpLock(op interface{}) {
	c.GetOpLock(op).Lock()
}

func (c *DirectCSIController) OpUnlock(op interface{}) {
	c.GetOpLock(op).Unlock()
}

func (c *DirectCSIController) GetOpLock(op interface{}) *sync.Mutex {
	var key string
	var ext string

	switch o := op.(type) {
	case addOp:
		key = o.Key
		ext = fmt.Sprintf("%v", o.AddFunc)
	case updateOp:
		key = o.Key
		ext = fmt.Sprintf("%v", o.UpdateFunc)
	case deleteOp:
		key = o.Key
		ext = fmt.Sprintf("%v", o.DeleteFunc)
	default:
		panic("unknown item in queue")
	}

	c.lockerLock.Lock()
	defer c.lockerLock.Unlock()
	lockKey := fmt.Sprintf("%s/%s", key, ext)
	if c.locker == nil {
		c.locker = map[string]*sync.Mutex{}
	}
	if _, ok := c.locker[lockKey]; !ok {
		c.locker[lockKey] = &sync.Mutex{}
	}
	return c.locker[lockKey]
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *DirectCSIController) handleErr(err error, op interface{}) {
	if err == nil {
		// Forget about the #AddRateLimited history of the op on every successful synchronization.
		// This ensures that future processing of updates for this op is not delayed because of
		// an outdated error history.
		c.queue.Forget(op)
		return
	}

	/* TODO: Determine if there is a maxium number of retries or time allowed before giving up
	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(op) < 5 {
		klog.Infof("Error syncing op %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the op will be processed later again.
		c.queue.AddRateLimited(op)
		return
	}

	c.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this op
	utilruntime.HandleError(err)
	klog.Infof("Dropping op %+v out of the queue: %v", op, err)
	*/
	klog.V(5).Infof("Error executing operation %+v: %+v", op, err)
	c.queue.AddRateLimited(op)
}

func (c *DirectCSIController) runController(ctx context.Context) {
	controllerFor := func(name string, objType runtime.Object, add addFunc, update updateFunc, delete deleteFunc) {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		resyncPeriod := c.ResyncPeriod

		lw := cache.NewListWatchFromClient(c.directcsiClient.DirectV1beta1().RESTClient(), name, "", fields.Everything())
		cfg := &cache.Config{
			Queue: cache.NewDeltaFIFOWithOptions(cache.DeltaFIFOOptions{
				KnownObjects:          indexer,
				EmitDeltaTypeReplaced: true,
			}),
			ListerWatcher:    lw,
			ObjectType:       objType,
			FullResyncPeriod: resyncPeriod,
			RetryOnError:     true,
			Process: func(obj interface{}) error {
				for _, d := range obj.(cache.Deltas) {
					switch d.Type {
					case cache.Sync, cache.Replaced, cache.Added, cache.Updated:
						if old, exists, err := indexer.Get(d.Object); err == nil && exists {
							key, err := cache.MetaNamespaceKeyFunc(d.Object)
							if err != nil {
								panic(err)
							}

							c.queue.Add(updateOp{
								OldObject:  old,
								NewObject:  d.Object,
								UpdateFunc: &update,
								Key:        key,
							})
							return indexer.Update(d.Object)
						} else {
							key, err := cache.MetaNamespaceKeyFunc(d.Object)
							if err != nil {
								panic(err)
							}

							c.queue.Add(addOp{
								Object:  d.Object,
								AddFunc: &add,
								Key:     key,
							})
							return indexer.Add(d.Object)
						}
					case cache.Deleted:
						key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(d.Object)
						if err != nil {
							panic(err)
						}

						c.queue.Add(deleteOp{
							Object:     d.Object,
							DeleteFunc: &delete,
							Key:        key,
						})
						return indexer.Delete(d.Object)
					}
				}
				return nil
			},
		}
		ctrlr := cache.New(cfg)

		defer utilruntime.HandleCrash()
		defer c.queue.ShutDown()

		klog.V(3).Infof("Starting %s controller", name)
		go ctrlr.Run(ctx.Done())

		if !cache.WaitForCacheSync(ctx.Done(), ctrlr.HasSynced) {
			utilruntime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
			return
		}

		for i := 0; i < c.threadiness; i++ {
			go wait.UntilWithContext(ctx, c.runWorker, time.Second)
		}

		<-ctx.Done()
		klog.V(3).Infof("Stopping %s controller", name)
	}

	if c.DirectCSIVolumeListener != nil {
		c.DirectCSIVolumeListener.InitializeKubeClient(c.kubeClient)
		c.DirectCSIVolumeListener.InitializeDirectCSIClient(c.directcsiClient)
		addFunc := func(ctx context.Context, obj interface{}) error {
			return c.DirectCSIVolumeListener.Add(ctx, obj.(*v1beta1.DirectCSIVolume))
		}
		updateFunc := func(ctx context.Context, old interface{}, new interface{}) error {
			return c.DirectCSIVolumeListener.Update(ctx, old.(*v1beta1.DirectCSIVolume), new.(*v1beta1.DirectCSIVolume))
		}
		deleteFunc := func(ctx context.Context, obj interface{}) error {
			return c.DirectCSIVolumeListener.Delete(ctx, obj.(*v1beta1.DirectCSIVolume))
		}
		go controllerFor("DirectCSIVolumes", &v1beta1.DirectCSIVolume{}, addFunc, updateFunc, deleteFunc)
	}
	if c.DirectCSIDriveListener != nil {
		c.DirectCSIDriveListener.InitializeKubeClient(c.kubeClient)
		c.DirectCSIDriveListener.InitializeDirectCSIClient(c.directcsiClient)
		addFunc := func(ctx context.Context, obj interface{}) error {
			return c.DirectCSIDriveListener.Add(ctx, obj.(*v1beta1.DirectCSIDrive))
		}
		updateFunc := func(ctx context.Context, old interface{}, new interface{}) error {
			return c.DirectCSIDriveListener.Update(ctx, old.(*v1beta1.DirectCSIDrive), new.(*v1beta1.DirectCSIDrive))
		}
		deleteFunc := func(ctx context.Context, obj interface{}) error {
			return c.DirectCSIDriveListener.Delete(ctx, obj.(*v1beta1.DirectCSIDrive))
		}
		go controllerFor("DirectCSIDrives", &v1beta1.DirectCSIDrive{}, addFunc, updateFunc, deleteFunc)
	}

	<-ctx.Done()
}
