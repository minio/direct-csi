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

package converter

import (
	"k8s.io/klog/v2"

	directv1alpha1 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1alpha1"
	directv1beta1 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta1"
	directv1beta2 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta2"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func downgradeDriveObject(fromVersion, toVersion string, convertedObject *unstructured.Unstructured) error {
	switch fromVersion {
	case versionV1Beta2:
		if err := driveDowngradeV1Beta2ToV1Beta1(convertedObject); err != nil {
			return err
		}
		fallthrough
	case versionV1Beta1:
		if toVersion == versionV1Beta1 {
			klog.V(2).Info("Successfully migrated")
			break
		}
		if err := driveDowngradeV1Beta1ToV1alpha1(convertedObject); err != nil {
			return err
		}
		fallthrough
	case versionV1Alpha1:
		if toVersion == versionV1Alpha1 {
			klog.V(2).Info("Successfully migrated")
			break
		}
	}
	return nil
}

func driveDowngradeV1Beta1ToV1alpha1(unstructured *unstructured.Unstructured) error {

	unstructuredObject := unstructured.Object

	var v1beta1DirectCSIDrive directv1beta1.DirectCSIDrive
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject, &v1beta1DirectCSIDrive); err != nil {
		return err
	}

	klog.V(4).Infof("Converting directcsidrive: %v to v1alpha1", v1beta1DirectCSIDrive.Name)

	var v1alpha1DirectCSIDrive directv1alpha1.DirectCSIDrive
	if err := directv1beta1.Convert_v1beta1_DirectCSIDrive_To_v1alpha1_DirectCSIDrive(&v1beta1DirectCSIDrive, &v1alpha1DirectCSIDrive, nil); err != nil {
		return err
	}

	v1alpha1DirectCSIDrive.TypeMeta = v1beta1DirectCSIDrive.TypeMeta
	convertedObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&v1alpha1DirectCSIDrive)
	if err != nil {
		return err
	}

	unstructured.Object = convertedObj
	return nil
}

func driveDowngradeV1Beta2ToV1Beta1(unstructured *unstructured.Unstructured) error {

	unstructuredObject := unstructured.Object

	var v1beta2DirectCSIDrive directv1beta2.DirectCSIDrive
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject, &v1beta2DirectCSIDrive); err != nil {
		return err
	}

	klog.V(4).Infof("Converting directcsidrive: %v to v1beta1", v1beta2DirectCSIDrive.Name)

	var v1beta1DirectCSIDrive directv1beta1.DirectCSIDrive
	if err := directv1beta2.Convert_v1beta2_DirectCSIDrive_To_v1beta1_DirectCSIDrive(&v1beta2DirectCSIDrive, &v1beta1DirectCSIDrive, nil); err != nil {
		return err
	}

	v1beta1DirectCSIDrive.TypeMeta = v1beta2DirectCSIDrive.TypeMeta
	convertedObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&v1beta1DirectCSIDrive)
	if err != nil {
		return err
	}

	unstructured.Object = convertedObj
	return nil
}
