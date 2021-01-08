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

package node

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	directv1alpha1 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1alpha1"
	"github.com/minio/direct-csi/pkg/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	vID := req.GetVolumeId()
	if vID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID missing in request")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "stagingTargetPath missing in request")
	}

	directCSIClient := utils.GetDirectCSIClient()
	dclient := directCSIClient.DirectCSIDrives()
	vclient := directCSIClient.DirectCSIVolumes()

	vol, err := vclient.Get(ctx, vID, metav1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// If already staged
	if vol.Status.StagingPath == stagingTargetPath {
		return &csi.NodeStageVolumeResponse{}, nil
	}

	drive, err := dclient.Get(ctx, vol.Status.Drive, metav1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	var size int64
	if val, ok := req.GetVolumeContext()["RequiredBytes"]; ok {
		size, err = strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "invalid volume size [%s]: %v", val, err)
		}
	}
	path := filepath.Join(drive.Status.Mountpoint, vID)
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	if err := mountVolume(ctx, path, stagingTargetPath, vID, size, false); err != nil {
		return nil, status.Errorf(codes.Internal, "failed stage volume: %v", err)
	}

	conditions := vol.Status.Conditions
	for _, c := range conditions {
		switch c.Type {
		case string(directv1alpha1.DirectCSIVolumeConditionPublished):
		case string(directv1alpha1.DirectCSIVolumeConditionStaged):
			c.Status = utils.BoolToCondition(true)
		}
	}
	vol.Status.HostPath = path
	vol.Status.StagingPath = stagingTargetPath

	if _, err := vclient.Update(ctx, vol, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	vID := req.GetVolumeId()
	if vID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID missing in request")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "stagingTargetPath missing in request")
	}

	directCSIClient := utils.GetDirectCSIClient()
	vclient := directCSIClient.DirectCSIVolumes()

	vol, err := vclient.Get(ctx, vID, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	if vol.Status.StagingPath == "" {
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	if err := utils.SafeUnmount(stagingTargetPath, nil); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	conditions := vol.Status.Conditions
	for i, c := range conditions {
		switch c.Type {
		case string(directv1alpha1.DirectCSIVolumeConditionPublished):
		case string(directv1alpha1.DirectCSIVolumeConditionStaged):
			conditions[i].Status = utils.BoolToCondition(false)
		}
	}

	vol.Status.HostPath = ""
	vol.Status.StagingPath = ""
	if _, err := directCSIClient.DirectCSIVolumes().Update(ctx, vol, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}
