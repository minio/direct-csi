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

package sys

type Device struct {
	// Populated from /sys
	Name      string
	Major     int
	Minor     int
	Size      uint64
	NGUID     string
	Partition int
	Removable bool
	ReadOnly  bool
	UUID      string
	WWID      string
	Model     string
	Serial    string
	Vendor    string
	DMName    string
	DMUUID    string
	MDUUID    string
	Virtual   bool
	Parent    string
	Master    string

	// Populated by reading device
	PTUUID            string
	PTType            string
	PartUUID          string
	FSUUID            string
	FSType            string
	TotalCapacity     int64
	FreeCapacity      int64
	LogicalBlockSize  int64
	PhysicalBlockSize int64

	// Populated from /proc/1/mountinfo
	MountPoints       []string
	FirstMountPoint   string
	FirstMountOptions []string
	Error             string
	Path              string
}

type MasterInfo struct {
	DMName string `json:"dmName,omitempty"`
	DMUUID string `json:"dmUUID,omitempty"`
	Parent string `json:"parent,omitempty"`
	Master string `json:"master,omitempty"`
}

type BlockDevice struct {
	Devname     string      `json:"devName,omitempty"`
	Devtype     string      `json:"devType,omitempty"`
	Partitions  []Partition `json:"partitions,omitempty"`
	DeviceError error       `json:"error, omitempty"`

	MasterInfo
	*DriveInfo `json:"driveInfo,omitempty"`
}

type Partition struct {
	PartitionNum  uint32 `json:"partitionNum,omitempty"`
	Type          string `json:"partitionType,omitempty"`
	TypeUUID      string `json:"partitionTypeUUID,omitempty"`
	PartitionGUID string `json:"partitionGUID,omitempty"`
	DiskGUID      string `json:"diskGUID,omitempty"`

	MasterInfo
	*DriveInfo `json:"driveInfo,omitempty"`
}

type DriveInfo struct {
	NumBlocks         uint64 `json:"numBlocks,omitempty"`
	StartBlock        uint64 `json:"startBlock,omitempty"`
	EndBlock          uint64 `json:"endBlock,omitempty"`
	TotalCapacity     uint64 `json:"totalCapacity,omitempty"`
	LogicalBlockSize  uint64 `json:"logicalBlockSize,omitempty"`
	PhysicalBlockSize uint64 `json:"physicalBlockSize,omitempty"`
	Path              string `json:"path,omitempty"`
	Major             uint32 `json:"major,omitempty"`
	Minor             uint32 `json:"minor",omitempty`

	*FSInfo `json:"fsInfo,omitempty"`
}

type FSInfo struct {
	FSType        string      `json:"fsType,omitempty"`
	TotalCapacity uint64      `json:"totalCapacity,omitempty"`
	FreeCapacity  uint64      `json:"freeCapacity,omitempty"`
	FSBlockSize   uint64      `json:"fsBlockSize,omitempty"`
	Mounts        []MountInfo `json:"mounts,omitempty"`
}

type MountInfo struct {
	Mountpoint        string   `json:"mountPoint,omitempty"`
	MountFlags        []string `json:"mountFlags,omitempty"`
	MountRoot         string   `json:"mountRoot,omitempty"`
	MountID           uint32   `json:"mountID,omitempty"`
	ParentID          uint32   `json:"parentID,omitempty"`
	MountSource       string   `json:"mountSource,omitempty"`
	SuperblockOptions []string `json:"superblockOptions,omitempty"`
	FSType            string   `json:"fsType,omitempty"`
	OptionalFields    []string `json:"optionalFields,omitempty"`
	Major             uint32   `json:"major,omitempty"`
	Minor             uint32   `json:"minor,omitempty"`
	DevName           string   `json:"devName,omitempty"`
	PartitionNum      uint     `json:"partitionNum,omitempty"`
}
