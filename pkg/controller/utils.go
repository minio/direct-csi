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

package controller

import (
	"sort"
	"strings"

	directv1alpha1 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1alpha1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golang/glog"
)

// FilterDrivesByVolumeRequest - Filters the CSI drives by create volume request
func FilterDrivesByVolumeRequest(volReq *csi.CreateVolumeRequest, csiDrives []directv1alpha1.DirectCSIDrive) ([]directv1alpha1.DirectCSIDrive, error) {
	capacityRange := volReq.GetCapacityRange()
	vCaps := volReq.GetVolumeCapabilities()
	fsType := ""
	if len(vCaps) > 0 {
		fsType = vCaps[0].GetMount().GetFsType()
	}

	filteredDrivesByFormat := FilterDrivesByRequestFormat(csiDrives)
	if len(filteredDrivesByFormat) == 0 {
		return []directv1alpha1.DirectCSIDrive{}, status.Error(codes.FailedPrecondition, "No csi drives are been added. Please use `add drives` plugin command to add the drives")
	}

	filDrNames := []string{}
	for _, f := range filteredDrivesByFormat {
		filDrNames = append(filDrNames, f.Status.NodeName+"-"+f.Name[0:8])
	}
	glog.Infof("filteredDrivesByFormat: %s", strings.Join(filDrNames, ","))

	capFilteredDrives := FilterDrivesByCapacityRange(capacityRange, filteredDrivesByFormat)
	if len(capFilteredDrives) == 0 {
		return []directv1alpha1.DirectCSIDrive{}, status.Error(codes.OutOfRange, "Invalid capacity range")
	}

	cFilDrNames := []string{}
	for _, f := range capFilteredDrives {
		cFilDrNames = append(cFilDrNames, f.Status.NodeName+"-"+f.Name[0:8])
	}
	glog.Infof("capacityFilteredDrives: %s", strings.Join(cFilDrNames, ","))

	fsFilteredDrives := FilterDrivesByFsType(fsType, capFilteredDrives)
	if len(fsFilteredDrives) == 0 {
		return []directv1alpha1.DirectCSIDrive{}, status.Errorf(codes.InvalidArgument, "Cannot find any drives by the fstype: %s", fsType)
	}

	fsFilDrNames := []string{}
	for _, f := range fsFilteredDrives {
		fsFilDrNames = append(fsFilDrNames, f.Status.NodeName+"-"+f.Name[0:8])
	}
	glog.Infof("fsFilteredDrives: %s", strings.Join(fsFilDrNames, ","))

	return fsFilteredDrives, nil
}

// FilterDrivesByCapacityRange - Filters the CSI drives by capacity range in the create volume request
func FilterDrivesByCapacityRange(capacityRange *csi.CapacityRange, csiDrives []directv1alpha1.DirectCSIDrive) []directv1alpha1.DirectCSIDrive {
	reqBytes := capacityRange.GetRequiredBytes()
	//limitBytes := capacityRange.GetLimitBytes()
	filteredDriveList := []directv1alpha1.DirectCSIDrive{}
	for _, csiDrive := range csiDrives {
		if csiDrive.Status.FreeCapacity >= reqBytes {
			filteredDriveList = append(filteredDriveList, csiDrive)
		}
	}
	return filteredDriveList
}

// FilterDrivesByRequestFormat - Selects the drives only if the requested format is empty/satisfied already.
func FilterDrivesByRequestFormat(csiDrives []directv1alpha1.DirectCSIDrive) []directv1alpha1.DirectCSIDrive {
	filteredDriveList := []directv1alpha1.DirectCSIDrive{}
	for _, csiDrive := range csiDrives {
		dStatus := csiDrive.Status.DriveStatus
		if dStatus == directv1alpha1.DriveStatusReady ||
			dStatus == directv1alpha1.DriveStatusInUse {
			filteredDriveList = append(filteredDriveList, csiDrive)
		}
	}
	return filteredDriveList
}

// FilterDrivesByFsType - Filters the CSI drives by filesystem
func FilterDrivesByFsType(fsType string, csiDrives []directv1alpha1.DirectCSIDrive) []directv1alpha1.DirectCSIDrive {
	if fsType == "" {
		return csiDrives
	}
	filteredDriveList := []directv1alpha1.DirectCSIDrive{}
	for _, csiDrive := range csiDrives {
		if csiDrive.Status.Filesystem == fsType {
			filteredDriveList = append(filteredDriveList, csiDrive)
		}
	}
	return filteredDriveList
}

// FilterDrivesByTopologyRequirements - selects the CSI drive by topology in the create volume request
func FilterDrivesByTopologyRequirements(volReq *csi.CreateVolumeRequest, csiDrives []directv1alpha1.DirectCSIDrive) (directv1alpha1.DirectCSIDrive, error) {
	tReq := volReq.GetAccessibilityRequirements()

	preferredXs := tReq.GetPreferred()
	requisiteXs := tReq.GetRequisite()

	// Sort the drives by free capacity [Descending]
	sort.SliceStable(csiDrives, func(i, j int) bool {
		return csiDrives[i].Status.FreeCapacity > csiDrives[j].Status.FreeCapacity
	})

	// Try to fullfill the preferred topology request, If not, fallback to requisite list.
	// Ref: https://godoc.org/github.com/container-storage-interface/spec/lib/go/csi#TopologyRequirement
	for _, preferredTop := range preferredXs {
		if selectedDrive, err := selectDriveByTopology(preferredTop, csiDrives); err == nil {
			return selectedDrive, nil
		}
	}

	for _, requisiteTop := range requisiteXs {
		if selectedDrive, err := selectDriveByTopology(requisiteTop, csiDrives); err == nil {
			return selectedDrive, nil
		}
	}

	if len(preferredXs) == 0 && len(requisiteXs) == 0 {
		return csiDrives[0], nil
	}

	return directv1alpha1.DirectCSIDrive{}, status.Error(codes.ResourceExhausted, "Cannot satisfy the topology constraint")
}

func selectDriveByTopology(top *csi.Topology, csiDrives []directv1alpha1.DirectCSIDrive) (directv1alpha1.DirectCSIDrive, error) {
	topSegments := top.GetSegments()
	for _, csiDrive := range csiDrives {
		driveSegments := csiDrive.Status.Topology
		if matchSegments(topSegments, driveSegments) {
			return csiDrive, nil
		}
	}
	return directv1alpha1.DirectCSIDrive{}, status.Error(codes.ResourceExhausted, "Cannot satisfy the topology constraint")
}

func matchSegments(topSegments, driveSegments map[string]string) bool {
	req := len(topSegments)
	match := 0
	for k, v := range topSegments {
		if dval, ok := driveSegments[k]; ok && dval == v {
			match = match + 1
		} else {
			break
		}
	}
	return req == match
}
