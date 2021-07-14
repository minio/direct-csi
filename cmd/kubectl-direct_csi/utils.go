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

package main

import (
	"context"
	jsonFormatter "encoding/json"
	"fmt"
	"strings"

	"github.com/fatih/color"

	directcsi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta2"
	"github.com/minio/direct-csi/pkg/converter"
	"github.com/minio/direct-csi/pkg/utils"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
	yamlFormatter "sigs.k8s.io/yaml"
)

type migrateFunc func(ctx context.Context, fromVersion string) error

// pretty printing utils
const dot = "•"

var (
	bold   = color.New(color.Bold).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
)

// ListVolumesInDrive returns a slice of all the DirectCSIVolumes created on a given DirectCSIDrive
func ListVolumesInDrive(drive directcsi.DirectCSIDrive, volumes *directcsi.DirectCSIVolumeList, vols []directcsi.DirectCSIVolume) []directcsi.DirectCSIVolume {
	for _, volume := range volumes.Items {
		if volume.Status.Drive == drive.ObjectMeta.Name {
			vols = append(vols, volume)
		}
	}
	return vols
}

func getAccessTierSet(accessTiers []string) ([]directcsi.AccessTier, error) {
	var atSet []directcsi.AccessTier
	for i := range accessTiers {
		if accessTiers[i] == "*" {
			return []directcsi.AccessTier{directcsi.AccessTierHot,
				directcsi.AccessTierWarm,
				directcsi.AccessTierCold}, nil
		}
		at, err := utils.ValidateAccessTier(strings.TrimSpace(accessTiers[i]))
		if err != nil {
			return atSet, err
		}
		atSet = append(atSet, at)
	}
	return atSet, nil
}

func printableString(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func printYAML(obj interface{}) error {
	formattedObj, err := yamlFormatter.Marshal(obj)
	if err != nil {
		return err
	}
	fmt.Println(string(formattedObj))
	return nil
}

func printJSON(obj interface{}) error {
	formattedObj, err := jsonFormatter.MarshalIndent(obj, "", "  ")
	if err != nil {
		klog.ErrorS(err, "error marshaling to JSON", "ouput", outputMode)
		return err
	}
	fmt.Println(string(formattedObj))
	return nil
}

func syncCRDObjects(ctx context.Context) error {
	crdClient := utils.GetCRDClient()

	supportedCRDs := []string{
		driveCRDName,
		volumeCRDName,
	}
	for _, crdName := range supportedCRDs {
		crd, err := crdClient.Get(ctx, crdName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		v := crd.GetLabels()[directcsi.Group+"/version"]
		if v == string(directcsi.Version) {
			// already upgraded to latest
			continue
		}

		if err := syncObjects(ctx, crd); err != nil {
			return err
		}

		SetCRDLabelKV(crd, directcsi.Group+"/version", directcsi.Version)
		if _, err := crdClient.Update(ctx, crd, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func syncObjects(ctx context.Context, crd *apiextensions.CustomResourceDefinition) error {
	storedVersions := crd.Status.StoredVersions
	if len(storedVersions) == 0 {
		// No objects stored
		return nil
	}

	if len(storedVersions) == 1 && storedVersions[0] == string(directcsi.Version) {
		// already latest
		return nil
	}

	info, err := utils.GetGroupKindVersions(directcsi.Group, crd.Spec.Names.Kind, "v1beta1", "v1alpha1")
	if err != nil {
		return err
	}
	fromVersion := info.Version

	migrateFn := func() migrateFunc {
		switch crd.Name {
		case driveCRDName:
			return migrateDriveObjects
		case volumeCRDName:
			return migrateVolumeObjects
		default:
			fn := func(_ context.Context, _ string) error {
				return fmt.Errorf("Unsupported crd: %v", crd.Name)
			}
			return fn
		}
	}()
	if err := migrateFn(ctx, fromVersion); err != nil {
		return err
	}

	klog.Infof("'%s' objects successfully synced", utils.Bold(crd.Name))
	return nil
}

func toUnstructured(obj interface{}) (unstructured.Unstructured, error) {
	unstructured := unstructured.Unstructured{}
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return unstructured, err
	}
	unstructured.Object = unstructuredObj
	return unstructured, nil
}

func migrateDriveObjects(ctx context.Context, fromVersion string) error {
	driveClient := utils.GetDirectCSIClient().DirectCSIDrives()
	var unstructuredList []unstructured.Unstructured
	driveList, err := driveClient.List(ctx, metav1.ListOptions{
		TypeMeta: utils.DirectCSIDriveTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	})
	if err != nil {
		klog.V(3).Infof("Error while syncing CRD versions in directcsidrives: %v", err)
		return err
	}
	drives := driveList.Items
	for _, d := range drives {
		unstructured, err := toUnstructured(&d)
		if err != nil {
			return err
		}
		unstructuredList = append(unstructuredList, unstructured)
	}

	for _, unstructured := range unstructuredList {
		unstructured.SetAPIVersion(strings.Join([]string{directcsi.Group, fromVersion}, "/"))
		if err := converter.Migrate(&unstructured, strings.Join([]string{directcsi.Group, directcsi.Version}, "/")); err != nil {
			klog.V(4).Infof("Error while syncing directcsidrive [%v]: %v", unstructured.GetName(), err)
			continue
		}
		var directCSIDrive directcsi.DirectCSIDrive
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &directCSIDrive); err != nil {
			klog.V(4).Infof("Error while syncing directcsidrive [%v]: %v", unstructured.GetName(), err)
			continue
		}
		updateOpts := metav1.UpdateOptions{
			TypeMeta: utils.DirectCSIDriveTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
		}
		if _, err := driveClient.Update(ctx, &directCSIDrive, updateOpts); err != nil {
			return err
		}
	}
	return nil
}

func migrateVolumeObjects(ctx context.Context, fromVersion string) error {
	volumeClient := utils.GetDirectCSIClient().DirectCSIVolumes()
	var unstructuredList []unstructured.Unstructured
	volumeList, err := volumeClient.List(ctx, metav1.ListOptions{
		TypeMeta: utils.DirectCSIDriveTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	})
	if err != nil {
		klog.V(3).Infof("Error while syncing CRD versions in directcsivolumes: %v", err)
		return err
	}
	volumes := volumeList.Items
	for _, v := range volumes {
		unstructured, err := toUnstructured(&v)
		if err != nil {
			return err
		}
		unstructuredList = append(unstructuredList, unstructured)
	}

	for _, unstructured := range unstructuredList {
		unstructured.SetAPIVersion(strings.Join([]string{directcsi.Group, fromVersion}, "/"))
		if err := converter.Migrate(&unstructured, strings.Join([]string{directcsi.Group, directcsi.Version}, "/")); err != nil {
			klog.V(4).Infof("Error while syncing directcsivolume [%v]: %v", unstructured.GetName(), err)
			continue
		}
		var directCSIVolume directcsi.DirectCSIVolume
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &directCSIVolume); err != nil {
			klog.V(4).Infof("Error while syncing directcsivolume [%v]: %v", unstructured.GetName(), err)
			continue
		}
		updateOpts := metav1.UpdateOptions{
			TypeMeta: utils.DirectCSIVolumeTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
		}
		if _, err := volumeClient.Update(ctx, &directCSIVolume, updateOpts); err != nil {
			return err
		}
	}
	return nil
}
