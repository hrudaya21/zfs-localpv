/*
Copyright 2020 The OpenEBS Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package backup

import (
	"sync"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"

	"time"

	clientset "github.com/openebs/zfs-localpv/pkg/generated/clientset/internalclientset"
	informers "github.com/openebs/zfs-localpv/pkg/generated/informer/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	masterURL  string
	kubeconfig string
)

// Start starts the zfsbackup controller.
func Start(controllerMtx *sync.RWMutex, stopCh <-chan struct{}) error {

	// Get in cluster config
	cfg, err := getClusterConfig(kubeconfig)
	if err != nil {
		return errors.Wrap(err, "error building kubeconfig")
	}

	// Building Kubernetes Clientset
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "error building kubernetes clientset")
	}

	// Building OpenEBS Clientset
	openebsClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "error building openebs clientset")
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	bkpInformerFactory := informers.NewSharedInformerFactory(openebsClient, time.Second*30)
	// Build() fn of all controllers calls AddToScheme to adds all types of this
	// clientset into the given scheme.
	// If multiple controllers happen to call this AddToScheme same time,
	// it causes panic with error saying concurrent map access.
	// This lock is used to serialize the AddToScheme call of all controllers.
	controllerMtx.Lock()

	controller, err := NewBkpControllerBuilder().
		withKubeClient(kubeClient).
		withOpenEBSClient(openebsClient).
		withBkpSynced(bkpInformerFactory).
		withBkpLister(bkpInformerFactory).
		withRecorder(kubeClient).
		withEventHandler(bkpInformerFactory).
		withWorkqueueRateLimiting().Build()

	// blocking call, can't use defer to release the lock
	controllerMtx.Unlock()

	if err != nil {
		return errors.Wrapf(err, "error building controller instance")
	}

	go kubeInformerFactory.Start(stopCh)
	go bkpInformerFactory.Start(stopCh)

	// Threadiness defines the number of workers to be launched in Run function
	return controller.Run(2, stopCh)
}

// GetClusterConfig return the config for k8s.
func getClusterConfig(kubeconfig string) (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		klog.Errorf("Failed to get k8s Incluster config. %+v", err)
		if kubeconfig == "" {
			return nil, errors.Wrap(err, "kubeconfig is empty")
		}
		cfg, err = clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
		if err != nil {
			return nil, errors.Wrap(err, "error building kubeconfig")
		}
	}
	return cfg, err
}
