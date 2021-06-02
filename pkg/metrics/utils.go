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

package metrics

import (
	"context"

	directcsi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta1"
	"github.com/minio/direct-csi/pkg/sys/xfs"

	"k8s.io/klog"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	tenantLabel = "direct.csi.min.io/tenant"
)

type xfsVolumeStatsGetter func(context.Context, *directcsi.DirectCSIVolume) (xfs.XFSVolumeStats, error)

func getXFSVolumeStats(ctx context.Context, vol *directcsi.DirectCSIVolume) (xfs.XFSVolumeStats, error) {
	xfsQuota := &xfs.XFSQuota{
		Path:      vol.Status.StagingPath,
		ProjectID: vol.Name,
	}
	volStats, err := xfsQuota.GetVolumeStats(ctx)
	if err != nil {
		return xfs.XFSVolumeStats{}, err
	}
	return volStats, nil
}

func publishVolumeStats(ctx context.Context, vol *directcsi.DirectCSIVolume, ch chan<- prometheus.Metric, xfsStatsFn xfsVolumeStatsGetter) {
	volStats, err := xfsStatsFn(ctx, vol)
	if err != nil {
		klog.V(3).Infof("Error while getting xfs volume stats: %v", err)
		return
	}

	getTenantName := func() string {
		labels := vol.ObjectMeta.GetLabels()
		for k, v := range labels {
			if k == tenantLabel {
				return v
			}
		}
		return ""
	}
	tenantName := getTenantName()

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(
			prometheus.BuildFQName("directcsi", "stats", "bytes_used"),
			"Total number of bytes used by the volume",
			[]string{"tenant", "volumeID", "node"}, nil),
		prometheus.GaugeValue,
		float64(volStats.UsedBytes), string(tenantName), vol.Name, vol.Status.NodeName,
	)

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(
			prometheus.BuildFQName("directcsi", "stats", "bytes_total"),
			"Total number of bytes allocated to the volume",
			[]string{"tenant", "volumeID", "node"}, nil),
		prometheus.GaugeValue,
		float64(volStats.TotalBytes), string(tenantName), vol.Name, vol.Status.NodeName,
	)
}
