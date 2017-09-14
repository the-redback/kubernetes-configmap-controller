package main

import (
	"flag"
	"log"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"path/filepath"
	"os"
	"Onboarding/Kube-ConfigMap-Watcher/pkg"
	"reflect"
)

func main() {
	runOutsideCluster := flag.Bool("run-outside-cluster", true, "Set this flag when running outside of the cluster.")
	flag.Parse()

	// creates the connection
	clientSet, err := newClientSet(*runOutsideCluster)
	if err != nil {
		log.Fatal(err)
	}


	// create the ConfigMap watcher
	configMapListWatcher := cache.NewListWatchFromClient(clientSet.CoreV1().RESTClient(), "configmaps", v1.NamespaceDefault, fields.Everything())

	// create the workqueue
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	indexer, informer := cache.NewIndexerInformer(configMapListWatcher, &v1.ConfigMap{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			if oldMap, oldOK := old.(*v1.ConfigMap); oldOK {
				if newMap, newOK := new.(*v1.ConfigMap); newOK {
					if !reflect.DeepEqual(oldMap.Data, newMap.Data) {
						if key, err := cache.MetaNamespaceKeyFunc(new); err == nil {
							log.Println("Queued Update event", key)
							queue.Add(key)
						} else {
							log.Println(err)
						}
					}
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta queue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				queue.Add(key)
			}
		},
	}, cache.Indexers{})

	controller := pkg.NewController(queue, indexer, informer)

	// We can now warm up the cache for initial synchronization.
	indexer.Add(&v1.Pod{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "myConfigMap",
			Namespace: v1.NamespaceDefault,
		},
	})

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	// Wait forever
	select {}
}

func newClientSet(runOutsideCluster bool) (*kubernetes.Clientset, error) {
	kubeConfigLocation := ""

	if runOutsideCluster == true {
		homeDir := os.Getenv("HOME")
		kubeConfigLocation = filepath.Join(homeDir, ".kube", "config")
	}

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigLocation)

	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}