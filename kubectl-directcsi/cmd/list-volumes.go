/*
 * This file is part of MinIO Direct CSI
 * Copyright (C) 2020, MinIO, Inc.
 *
 * This code is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License, version 3,
 * as published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License, version 3,
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 *
 */

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"

	directv1alpha1 "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1alpha1"
	"github.com/minio/kubectl-directcsi/util"
	"github.com/minio/minio-go/v6/pkg/set"
	"github.com/minio/minio/pkg/ellipses"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	csiListVolumesDesc = `
 'volumes' command lists volumes provisioned by DirectCSI.`
	csiListVolumesExample = `  kubectl directcsi volumes --drives /dev/nvme* --nodes 'rack*'`
)

type csiListVolumesCmd struct {
	out     io.Writer
	errOut  io.Writer
	output  bool
	verbose bool
	drives  string
	nodes   string
}

func newListVolumesCmd(out io.Writer, errOut io.Writer) *cobra.Command {
	l := &csiListVolumesCmd{out: out, errOut: errOut}

	cmd := &cobra.Command{
		Use:     "volumes",
		Short:   "List volumes provisioned.",
		Long:    csiListVolumesDesc,
		Example: csiListVolumesExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			return l.run(args)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&l.nodes, "nodes", "n", "", "list volumes provisioned from drives on particular node. Defaults to all")
	f.StringVarP(&l.drives, "status", "s", "", "list volumes provisioned from particular drive. Defaults to all")
	f.BoolVarP(&l.verbose, "verbose", "v", false, "show detailed volume information ")

	return cmd
}

// run initializes local config and installs MinIO Operator to Kubernetes cluster.
func (l *csiListVolumesCmd) run(args []string) error {
	ctx := context.Background()

	directCSIClient := util.GetDirectCSIClient()
	drives, err := directCSIClient.DirectCSIDrives().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("could not list all drives: %v", err)
	}

	volumes, err := directCSIClient.DirectCSIVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("could not list all drives: %v", err)
	}

	if !ellipses.HasEllipses(l.nodes) {
		return fmt.Errorf("please provide --node flag in ellipses format, e.g. `myhost{1...4}`")
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeader([]string{"VOLUME", "NODENAME", "DRIVE", "CAPACITY", "STATUS"})

	var nodes []string
	if l.nodes != "" {
		pattern, err := ellipses.FindEllipsesPatterns(l.nodes)
		if err != nil {
			return err
		}
		for _, p := range pattern {
			nodes = append(nodes, p.Expand()...)
		}
	}
	vols := make([]directv1alpha1.DirectCSIVolume, 0)
	nodeSet := set.CreateStringSet(nodes...)
	if !nodeSet.IsEmpty() {
		for _, drive := range drives.Items {
			if nodeSet.Contains(drive.OwnerNode) {
				match, _ := regexp.Match(l.drives, []byte(drive.Path))
				if match {
					vols = util.ListVolumesInDrive(drive, volumes)
				}
			}
		}
	} else {
		for _, drive := range drives.Items {
			match, _ := regexp.Match(l.drives, []byte(drive.Path))
			if match {
				vols = util.ListVolumesInDrive(drive, volumes)
			}
		}
	}

	for _, v := range vols {
		table.Append([]string{
			v.Name,
			v.OwnerNode,
			v.OwnerDrive.Path,
			strconv.FormatInt(v.TotalCapacity, 10),
			"", //TODO: Add Bind Status
		})
	}

	table.Render()

	return nil
}
