// This file is part of MinIO Kubernetes Cloud
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

package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	_ "github.com/golang/glog"
)

var Version string

// flags
var (
	identity = "direct.csi.driver.min.io"
	nodeID   = ""
	rack     = "default"
	zone     = "default"
	region   = "default"
	endpoint = "unix://csi/csi.sock"
)

var driverCmd = &cobra.Command{
	Use:   os.Args[0],
	Short: "CSI driver for provisioning from JBOD(s) directly",
	Long: fmt.Sprintf(`
This Container Storage Interface (CSI) driver provides just a bunch of drives (JBODs) as volumes consumable within containers. This driver does not manage the lifecycle of the data or the backing of the disk itself. It only acts as the middle-man between a drive and a container runtime.

This driver is rack, region and zone aware i.e., a workload requesting volumes with constraints on rack, region or zone will be scheduled to run only within the constraints. This is useful for requesting volumes that need to be within a specified failure domain (rack, region or zone)

For more information, use '%s man [sched | examples | ...]'
`, os.Args[0]),
	SilenceUsage: true,
	RunE: func(c *cobra.Command, args []string) error {
		return driver(args)
	},
	Version: Version,
}

func init() {
	if driverCmd.Version == "" {
		driverCmd.Version = "dev"
	}

	viper.AutomaticEnv()
	// parse the go default flagset to get flags for glog and other packages in future
	driverCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	// defaulting this to true so that logs are printed to console
	flag.Set("logtostderr", "true")

	driverCmd.PersistentFlags().StringVarP(&identity, "identity", "i", identity, "identity of this direct-csi-driver")
	driverCmd.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", endpoint, "endpoint at which direct-csi-driver is listening")
	driverCmd.PersistentFlags().StringVarP(&nodeID, "node-id", "n", nodeID, "identity of the node in which direct-csi-driver is running")
	driverCmd.PersistentFlags().StringVarP(&rack, "rack", "", rack, "identity of the rack in which this direct-csi-driver is running")
	driverCmd.PersistentFlags().StringVarP(&zone, "zone", "", zone, "identity of the zone in which this direct-csi-driver is running")
	driverCmd.PersistentFlags().StringVarP(&region, "region", "", region, "identity of the region in which this direct-csi-driver is running")

	driverCmd.PersistentFlags().MarkHidden("alsologtostderr")
	driverCmd.PersistentFlags().MarkHidden("log_backtrace_at")
	driverCmd.PersistentFlags().MarkHidden("log_dir")
	driverCmd.PersistentFlags().MarkHidden("logtostderr")
	driverCmd.PersistentFlags().MarkHidden("master")
	driverCmd.PersistentFlags().MarkHidden("stderrthreshold")
	driverCmd.PersistentFlags().MarkHidden("vmodule")

	// suppress the incorrect prefix in glog output
	flag.CommandLine.Parse([]string{})
	viper.BindPFlags(driverCmd.PersistentFlags())
}

func Execute() error {
	return driverCmd.Execute()
}

/*
Driver presents a bunch of drives as volumes to containers requesting it.
A bunch of drives can be representing using glob notation. For eg.

1. /mnt/drive{1...32}/path

This presents 32 drives, whose subdirectory (./path) is the root directory
for the CSI driver to operate

2. /mnt/drive{1...32}/path{1...4}

This presents 32 drives, whose subdirectories path1, path2, path3, path4 are
provided as root directories for the CSI driver. This driver will behave as
if it was operating with 128 drives (32 * 4)

The driver carves out a unique volume for a particular container from this path
by creating a sub-directory. The volume is identified by the subdirectory name.
It employs a simple round-robin approach to provisioning from each of the drives
given to it.

*/
