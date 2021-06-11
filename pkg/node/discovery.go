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

package node

import (
	"context"
	"fmt"
	"strings"

	directcsi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta2"
	"github.com/minio/direct-csi/pkg/sys"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	simd "github.com/minio/sha256-simd"
)

const (
	loopBackDeviceCount = 4
)

func makeName(nodeID, path string) string {
	driveName := strings.Join([]string{nodeID, path}, "-")
	return fmt.Sprintf("%x", simd.Sum256([]byte(driveName)))
}

func isMountedOutside(mountPoints []string) bool {
	for _, mountPoint := range mountPoints {
		if !strings.HasPrefix(mountPoint, "/var/lib/direct-csi/") {
			return true
		}
	}
	return false
}

func toDirectCSIDrive(nodeID string, device *sys.Device) directcsi.DirectCSIDrive {
	driveStatus := directcsi.DriveStatusAvailable
	if isMountedOutside(device.MountPoints) || device.Error != "" {
		driveStatus = directcsi.DriveStatusUnavailable
	}
	blockInitializationStatus := metav1.ConditionTrue
	if device.Error != "" {
		blockInitializationStatus = metav1.ConditionFalse
	}
	mounted := metav1.ConditionFalse
	if device.FirstMountPoint != "" {
		mounted = metav1.ConditionTrue
	}
	formatted := metav1.ConditionFalse
	if device.FSType != "" {
		formatted = metav1.ConditionTrue
	}

	return directcsi.DirectCSIDrive{
		ObjectMeta: metav1.ObjectMeta{
			Name: makeName(nodeID, device.Path),
		},
		Status: directcsi.DirectCSIDriveStatus{
			DriveStatus:       directcsi.DriveStatus(driveStatus),
			Filesystem:        device.FSType,
			FreeCapacity:      device.FreeCapacity,
			AllocatedCapacity: device.TotalCapacity - device.FreeCapacity,
			LogicalBlockSize:  device.LogicalBlockSize,
			ModelNumber:       device.Model,
			MountOptions:      device.FirstMountOptions,
			Mountpoint:        device.FirstMountPoint,
			NodeName:          nodeID,
			Path:              device.Path,
			PhysicalBlockSize: device.PhysicalBlockSize,
			RootPartition:     device.Name,
			SerialNumber:      device.Serial,
			TotalCapacity:     device.TotalCapacity,
			Conditions: []metav1.Condition{
				{
					Type:               string(directcsi.DirectCSIDriveConditionOwned),
					Status:             metav1.ConditionFalse,
					Reason:             string(directcsi.DirectCSIDriveReasonNotAdded),
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               string(directcsi.DirectCSIDriveConditionMounted),
					Status:             mounted,
					Message:            device.FirstMountPoint,
					Reason:             string(directcsi.DirectCSIDriveReasonNotAdded),
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               string(directcsi.DirectCSIDriveConditionFormatted),
					Status:             formatted,
					Message:            "xfs",
					Reason:             string(directcsi.DirectCSIDriveReasonNotAdded),
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               string(directcsi.DirectCSIDriveConditionInitialized),
					Status:             blockInitializationStatus,
					Message:            device.Error,
					Reason:             string(directcsi.DirectCSIDriveReasonInitialized),
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}
}

func findDrives(ctx context.Context, nodeID string, procfs string, loopBackOnly bool) ([]directcsi.DirectCSIDrive, error) {
	drives := []directcsi.DirectCSIDrive{}

	if loopBackOnly {
		// Flush the existing loopback setups
		if err := sys.FlushLoopBackReservations(); err != nil {
			return drives, err
		}
		// Reserve loopbacks
		if err := sys.ReserveLoopbackDevices(loopBackDeviceCount); err != nil {
			return drives, err
		}
	}

	devices, err := sys.FindDevices(ctx, loopBackOnly)
	if err != nil {
		return drives, err
	}
	for _, device := range devices {
		drives = append(drives, toDirectCSIDrive(nodeID, device))
	}
	return drives, nil
}
