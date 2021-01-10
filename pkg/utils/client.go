// This file is part of MinIO Direct CSI
// Copyright (c) 2020 MinIO, Inc.
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

package utils

import (
	direct "github.com/minio/direct-csi/pkg/clientset"
	directv1alpha1 "github.com/minio/direct-csi/pkg/clientset/typed/direct.csi.min.io/v1alpha1"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/golang/glog"
)

func GetKubeClient() kubernetes.Interface {
	return kubeClient
}

func GetDirectCSIClient() directv1alpha1.DirectV1alpha1Interface {
	return directCSIClient
}

func GetDirectClientset() direct.Interface {
	return directClientset
}

func GetCRDClient() apiextensions.CustomResourceDefinitionInterface {
	return crdClient
}

func GetDiscoveryClient() discovery.DiscoveryInterface {
	return discoveryClient
}

func GetMetadataClient() metadata.Interface {
	return metadataClient
}

func GetClientForNonCoreGroupKindVersions(group, kind string, versions ...string) (rest.Interface, *schema.GroupVersionKind, error) {
	gvk, err := GetGroupKindVersions(group, kind, versions...)
	if err != nil {
		return nil, nil, err
	}

	gv := &schema.GroupVersion{
		Group:   gvk.Group,
		Version: gvk.Version,
	}
	kubeConfig := GetKubeConfig()
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		config, err = rest.InClusterConfig()
		if err != nil {
			glog.Fatalf("could not find client configuration: %v", err)
		}
		glog.Infof("obtained client config successfully")
	}
	config.GroupVersion = gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	client, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, nil, err
	}
	return client, gvk, nil
}
