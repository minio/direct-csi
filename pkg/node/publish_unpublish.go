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
	"fmt"
	"github.com/golang/glog"
	"github.com/minio/direct-csi/pkg/utils"
	"k8s.io/utils/mount"
	"os"
	"path/filepath"
	"time"
)

func (n *NodeServer) PublishVolume(ctx context.Context, stagingPath, containerPath string, readOnly bool) error {
	if err := os.MkdirAll(containerPath, 0755); err != nil {
		return err
	}

	if _, err := os.Lstat(containerPath); err != nil {
		return err
	}

	mounter := mount.New("")

	glog.V(5).Infof("Obtaining lock for publishing volume path %s", containerPath)
	if n.nsLockMap.lockLoop(ctx, containerPath, 1*time.Minute, 2*time.Second) {
		defer n.nsLockMap.unlock(containerPath)
		shouldBindMount := true
		mountPoints, mntErr := mounter.List()
		if mntErr != nil {
			return mntErr
		}
		for _, mp := range mountPoints {
			abPath, _ := filepath.Abs(mp.Path)
			if containerPath == abPath {
				shouldBindMount = false
				break
			}
		}

		if shouldBindMount {
			opts := []string{"bind", "prjquota"}
			if readOnly {
				opts = append(opts, "ro")
			}
			if err := mounter.Mount(stagingPath, containerPath, "", opts); err != nil {
				return err
			}
		}
		return nil
	}

	return fmt.Errorf("Failed to obtain ns lock while Publishing volume for %s", containerPath)
}

func (n *NodeServer) UnpublishVolume(ctx context.Context, containerPath string) error {
	if n.nsLockMap.lockLoop(ctx, containerPath, 1*time.Minute, 2*time.Second) {
		defer n.nsLockMap.unlock(containerPath)
		if _, err := os.Lstat(containerPath); err != nil {
			return err
		}
		return utils.UnmountIfMounted(containerPath)
	}
	return fmt.Errorf("Failed to obtain ns lock while Unpublishing volume path %s", containerPath)
}
