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
	"os"
	"path/filepath"
	"strings"

	directcsi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta1"
	"github.com/minio/direct-csi/pkg/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
)

func (n *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(3).Infof("NodeStageVolumeRequest: %v", req)
	vID := req.GetVolumeId()
	if vID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID missing in request")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "stagingTargetPath missing in request")
	}

	directCSIClient := n.directcsiClient.DirectV1beta1()
	dclient := directCSIClient.DirectCSIDrives()
	vclient := directCSIClient.DirectCSIVolumes()

	vol, err := vclient.Get(ctx, vID, metav1.GetOptions{
		TypeMeta: utils.DirectCSIVolumeTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	})
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	drive, err := dclient.Get(ctx, vol.Status.Drive, metav1.GetOptions{
		TypeMeta: utils.DirectCSIDriveTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	})
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	path := filepath.Join(drive.Status.Mountpoint, vID)
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	size := vol.Status.TotalCapacity
	if err := n.mounter.MountVolume(ctx, path, stagingTargetPath, vID, size, false); err != nil {
		return nil, status.Errorf(codes.Internal, "failed stage volume: %v", err)
	}

	conditions := vol.Status.Conditions
	for i, c := range conditions {
		switch c.Type {
		case string(directcsi.DirectCSIVolumeConditionReady):
			conditions[i].Status = utils.BoolToCondition(true)
			conditions[i].Reason = directcsi.DirectCSIVolumeReasonReady
		case string(directcsi.DirectCSIVolumeConditionPublished):
		case string(directcsi.DirectCSIVolumeConditionStaged):
			conditions[i].Status = utils.BoolToCondition(true)
			conditions[i].Reason = directcsi.DirectCSIVolumeReasonInUse
		}
	}

	vol.Status.HostPath = path
	vol.Status.StagingPath = stagingTargetPath

	if _, err := vclient.Update(ctx, vol, metav1.UpdateOptions{
		TypeMeta: utils.DirectCSIVolumeTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	}); err != nil {
		return nil, err
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(3).Infof("NodeUnStageVolumeRequest: %v", req)
	vID := req.GetVolumeId()
	if vID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID missing in request")
	}
	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "stagingTargetPath missing in request")
	}

	directCSIClient := n.directcsiClient.DirectV1beta1()
	vclient := directCSIClient.DirectCSIVolumes()

	vol, err := vclient.Get(ctx, vID, metav1.GetOptions{
		TypeMeta: utils.DirectCSIVolumeTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	})
	if err != nil {
		if errors.IsNotFound(err) {
			return &csi.NodeUnstageVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := n.mounter.UnmountVolume(stagingTargetPath); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	conditions := vol.Status.Conditions
	for i, c := range conditions {
		switch c.Type {
		case string(directcsi.DirectCSIVolumeConditionPublished):
		case string(directcsi.DirectCSIVolumeConditionStaged):
			conditions[i].Status = utils.BoolToCondition(false)
			conditions[i].Reason = string(directcsi.DirectCSIVolumeReasonNotInUse)
		case string(directcsi.DirectCSIVolumeConditionReady):
		}
	}

	vol.Status.HostPath = ""
	vol.Status.StagingPath = ""
	if _, err := directCSIClient.DirectCSIVolumes().Update(ctx, vol, metav1.UpdateOptions{
		TypeMeta: utils.DirectCSIVolumeTypeMeta(strings.Join([]string{directcsi.Group, directcsi.Version}, "/")),
	}); err != nil {
		return nil, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}
