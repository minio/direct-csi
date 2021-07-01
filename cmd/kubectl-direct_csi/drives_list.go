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
	"fmt"
	"os"
	"sort"
	"strings"

	directcsi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1beta2"
	"github.com/minio/direct-csi/pkg/sys"
	"github.com/minio/direct-csi/pkg/utils"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/dustin/go-humanize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"k8s.io/klog/v2"
)

var listDrivesCmd = &cobra.Command{
	Use:   "list",
	Short: "list drives in the DirectCSI cluster",
	Long:  "",
	Example: `
# Filter all nvme drives in all nodes 
$ kubectl direct-csi drives ls --drives='/dev/nvme*'

# Filter all ready drives 
$ kubectl direct-csi drives ls --status=ready

# Filter all drives from a particular node
$ kubectl direct-csi drives ls --nodes=directcsi-1

# Combine multiple filters using multi-arg
$ kubectl direct-csi drives ls --nodes=directcsi-1 --nodes=othernode-2 --status=available

# Combine multiple filters using csv
$ kubectl direct-csi drives ls --nodes=directcsi-1,othernode-2 --status=ready

# Filter all drives based on access-tier
$ kubectl direct-csi drives drives ls --access-tier="hot"

# Filter all drives with access-tier being set
$ kubectl direct-csi drives drives ls --access-tier="*"

# Filter a drive by it's drive-id [drive-id will be displayed with '-o wide' set on the command]
$ kubectl direct-csi drives ls <drive_id>

# Filter more than one drive by their drive-ids [drive-id will be displayed with '-o wide' set on the command]
$ kubectl direct-csi drives ls <drive_id_1> <drive_id_2>
`,
	RunE: func(c *cobra.Command, args []string) error {
		return listDrives(c.Context(), args)
	},
	Aliases: []string{
		"ls",
	},
}

var (
	all = false
)

const (
	directCSIPartitionInfix = "-part-"
)

func init() {
	listDrivesCmd.PersistentFlags().StringSliceVarP(&drives, "drives", "d", drives, "glob prefix match for drive paths")
	listDrivesCmd.PersistentFlags().StringSliceVarP(&nodes, "nodes", "n", nodes, "glob prefix match for node names")
	listDrivesCmd.PersistentFlags().StringSliceVarP(&status, "status", "s", status, "glob prefix match for drive status")

	listDrivesCmd.PersistentFlags().BoolVarP(&all, "all", "a", all, "list all drives (including unavailable)")

	listDrivesCmd.PersistentFlags().StringSliceVarP(&accessTiers, "access-tier", "", accessTiers, "filter based on access-tier")
}

func listDrives(ctx context.Context, args []string) error {
	utils.Init()

	filteredDrives := []directcsi.DirectCSIDrive{}
	directClient := utils.GetDirectCSIClient()

	if len(args) > 0 {
		for _, driveNameArg := range args {
			driveName := strings.TrimSpace(driveNameArg)
			drive, err := directClient.DirectCSIDrives().Get(ctx, driveName, metav1.GetOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					return err
				}
				klog.Errorf("No resource of %s found by the name %s", bold("DirectCSIDrive"), driveName)
				continue
			}
			filteredDrives = append(filteredDrives, *drive)
		}
	} else {
		driveList, err := directClient.DirectCSIDrives().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}

		if len(driveList.Items) == 0 {
			klog.Errorf("No resource of %s found\n", bold("DirectCSIDrive"))
			return fmt.Errorf("No resources found")
		}

		accessTierSet, aErr := getAccessTierSet(accessTiers)
		if aErr != nil {
			return aErr
		}

		for _, d := range driveList.Items {
			if !all {
				if d.Status.DriveStatus == directcsi.DriveStatusUnavailable || d.Status.DriveStatus == directcsi.DriveStatusUnidentified {
					continue
				}
			}
			if d.MatchGlob(nodes, drives, status) {
				if d.MatchAccessTier(accessTierSet) {
					filteredDrives = append(filteredDrives, d)
				}
			}
		}

		sort.SliceStable(filteredDrives, func(i, j int) bool {
			d1 := filteredDrives[i]
			d2 := filteredDrives[j]

			if v := strings.Compare(d1.Status.NodeName, d2.Status.NodeName); v != 0 {
				return v < 0
			}

			if v := strings.Compare(d1.Status.Path, d2.Status.Path); v != 0 {
				return v < 0
			}

			return strings.Compare(string(d1.Status.DriveStatus), string(d2.Status.DriveStatus)) < 0
		})
	}

	wrappedDriveList := directcsi.DirectCSIDriveList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "List",
			APIVersion: strings.Join([]string{directcsi.Group, directcsi.Version}, "/"),
		},
		Items: filteredDrives,
	}
	if yaml {
		if err := printYAML(wrappedDriveList); err != nil {
			klog.ErrorS(err, "error marshaling drives", "format", outputMode)
			return err
		}
		return nil
	}
	if json {
		if err := printJSON(wrappedDriveList); err != nil {
			klog.ErrorS(err, "error marshaling drives", "format", outputMode)
			return err
		}
		return nil
	}

	headers := func() []interface{} {
		header := []interface{}{
			"DRIVE",
			"CAPACITY",
			"ALLOCATED",
			"FILESYSTEM",
			"VOLUMES",
			"NODE",
			"ACCESS-TIER",
			"STATUS",
			"",
		}
		if wide {
			header = append(header, "DRIVE ID")
		}
		return header
	}()

	text.DisableColors()
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row(headers))

	style := table.StyleColoredDark
	style.Color.IndexColumn = text.Colors{text.FgHiBlue, text.BgHiBlack}
	style.Color.Header = text.Colors{text.FgHiBlue, text.BgHiBlack}
	t.SetStyle(style)

	driveName := func(val string) string {
		dr := strings.ReplaceAll(val, sys.DirectCSIDevRoot+"/", "")
		dr = strings.ReplaceAll(dr, sys.HostDevRoot+"/", "")
		return strings.ReplaceAll(dr, directCSIPartitionInfix, "")
	}

	volList, err := directClient.DirectCSIVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, d := range filteredDrives {
		volumes := 0
		for _, v := range volList.Items {
			if v.Status.Drive == d.Name {
				volumes++
			}
		}

		msg := ""
		dr := func(val string) string {
			dr := driveName(val)
			for _, c := range d.Status.Conditions {
				if c.Type == string(directcsi.DirectCSIDriveConditionInitialized) {
					if c.Status != metav1.ConditionTrue {
						msg = c.Message
						continue
					}
				}
				if c.Type == string(directcsi.DirectCSIDriveConditionOwned) {
					if c.Status != metav1.ConditionTrue {
						msg = c.Message
						continue
					}
				}
			}
			return strings.ReplaceAll("/dev/"+dr, directCSIPartitionInfix, "")
		}(d.Status.Path)
		drStatus := d.Status.DriveStatus
		if msg != "" {
			drStatus = drStatus + "*"
			msg = strings.ReplaceAll(msg, d.Name, "")
			msg = strings.ReplaceAll(msg, "/var/lib/direct-csi/devices", "/dev")
			msg = strings.ReplaceAll(msg, directCSIPartitionInfix, "")
			msg = strings.Split(msg, "\n")[0]
		}

		emptyOrVal := func(val int) string {
			if val == 0 {
				return "-"
			}
			return fmt.Sprintf("%d", val)
		}
		emptyOrBytes := func(val int64) string {
			if val == 0 {
				return "-"
			}
			return humanize.IBytes(uint64(val))
		}
		t.AppendRow([]interface{}{
			dr,                                       //DRIVE
			emptyOrBytes(d.Status.TotalCapacity),     //CAPACITY
			emptyOrBytes(d.Status.AllocatedCapacity), //ALLOCATED
			printableString(d.Status.Filesystem),
			emptyOrVal(volumes), //VOLUMES
			d.Status.NodeName,   //SERVER
			func(drive directcsi.DirectCSIDrive) string {
				if drive.Status.AccessTier == directcsi.AccessTierUnknown {
					return "-"
				}
				return strings.ToLower(string(drive.Status.AccessTier))
			}(d), //ACCESS-TIER
			utils.Bold(drStatus), //STATUS
			msg,                  //MESSAGE
			func() string {
				if wide {
					return d.Name
				}
				return ""
			}(),
		})
	}

	t.Render()
	return nil
}
