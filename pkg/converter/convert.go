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
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	directcsi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type migrateFunc func(fromVersion, toVersion string, Object *unstructured.Unstructured) error

var (
	ErrCRDKindNotSupported = errors.New("Unsupported CRD Kind")
)

func convertDriveCRD(Object *unstructured.Unstructured, toVersion string) (*unstructured.Unstructured, metav1.Status) {
	convertedObject := Object.DeepCopy()
	if err := Migrate(convertedObject, toVersion); err != nil {
		return nil, statusErrorWithMessage(err.Error())
	}
	return convertedObject, statusSucceed()
}

func convertVolumeCRD(Object *unstructured.Unstructured, toVersion string) (*unstructured.Unstructured, metav1.Status) {
	convertedObject := Object.DeepCopy()
	if err := Migrate(convertedObject, toVersion); err != nil {
		return nil, statusErrorWithMessage(err.Error())
	}
	return convertedObject, statusSucceed()
}

func Migrate(convertedObject *unstructured.Unstructured, toVersion string) error {

	fromVersion := convertedObject.GetAPIVersion()
	migrateFn, err := getMigrateFunc(fromVersion, toVersion)
	if err != nil {
		return err
	}

	// migrate the CRDs
	if err := migrateFn(fromVersion, toVersion, convertedObject); err != nil {
		return err
	}
	convertedObject.SetAPIVersion(toVersion)

	labels := convertedObject.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	if _, ok := labels[directcsi.Group+"/version"]; !ok {
		labels[directcsi.Group+"/version"] = filepath.Base(fromVersion)
	}
	convertedObject.SetLabels(labels)

	return nil
}

func getMigrateFunc(fromVersion, toVersion string) (migrateFunc, error) {
	var migrateFn migrateFunc
	getIndex := func(version string) int {
		for i := range supportedVersions {
			if supportedVersions[i] == version {
				return i
			}
		}
		return -1
	}

	shouldUpgrade := func() (bool, error) {
		fromIndex := getIndex(fromVersion)
		if fromIndex == -1 {
			return false, fmt.Errorf("Invalid fromVersion: %s", fromVersion)
		}

		toIndex := getIndex(toVersion)
		if toIndex == -1 {
			return false, fmt.Errorf("Invalid toVersion: %s", toVersion)
		}

		if fromIndex == toIndex {
			return false, fmt.Errorf("conversion from a version to itself should not call the webhook: %s", toVersion)
		}

		if fromIndex > toIndex {
			return false, nil
		}
		return true, nil
	}

	upgrade, err := shouldUpgrade()
	if err != nil {
		return migrateFn, err
	}

	migrateFn = func() migrateFunc {
		if upgrade {
			return upgradeObject
		}
		return downgradeObject
	}()

	return migrateFn, nil
}

func getCRDKind(convertedObject *unstructured.Unstructured) CRDKind {
	crdKindUntyped := convertedObject.GetKind()
	cleanKindStr := strings.ReplaceAll(crdKindUntyped, " ", "")
	return CRDKind(cleanKindStr)
}

func upgradeObject(fromVersion, toVersion string, convertedObject *unstructured.Unstructured) error {
	crdKind := getCRDKind(convertedObject)
	switch crdKind {
	case DriveCRDKind:
		if err := upgradeDriveObject(fromVersion, toVersion, convertedObject); err != nil {
			return err
		}
	case VolumeCRDKind:
		if err := upgradeVolumeObject(fromVersion, toVersion, convertedObject); err != nil {
			return err
		}
	default:
		return ErrCRDKindNotSupported
	}

	return nil
}

func downgradeObject(fromVersion, toVersion string, convertedObject *unstructured.Unstructured) error {
	crdKind := getCRDKind(convertedObject)
	switch crdKind {
	case DriveCRDKind:
		if err := downgradeDriveObject(fromVersion, toVersion, convertedObject); err != nil {
			return err
		}
	case VolumeCRDKind:
		if err := downgradeVolumeObject(fromVersion, toVersion, convertedObject); err != nil {
			return err
		}
	default:
		return ErrCRDKindNotSupported
	}
	return nil
}
