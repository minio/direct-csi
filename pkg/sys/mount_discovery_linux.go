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

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (b *BlockDevice) probeMountInfo(major, minor uint32, deviceMap map[string]*Device) ([]MountInfo, error) {
	mounts, err := ProbeMountInfo()
	if err != nil {
		return nil, err
	}
	toRet := []MountInfo{}
	for _, m := range mounts {
		if major == m.Major && minor == m.Minor {
			if strings.HasPrefix(m.MountSource, "/dev/mapper/") {
				for _, d := range deviceMap {
					if d.Major == int(m.Major) && d.Minor == int(m.Minor) {
						m.MountSource = "/dev/" + d.Name
						break
					}
				}
			}
			toRet = append(toRet, m)
			continue
		}
		if b.HostDrivePath() == m.MountRoot {
			toRet = append(toRet, m)
			continue
		}
	}
	return toRet, nil
}

// ProbeMountInfo - fetches the list of mounted filesystems on particular node
func ProbeMountInfo() ([]MountInfo, error) {
	mountinfoFile := filepath.Join(DefaultProcFS, "1", "mountinfo")
	f, err := os.Open(mountinfoFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mounts := []MountInfo{}
	fbuf := bufio.NewReader(f)

	for {
		line, err := fbuf.ReadString(byte('\n'))
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break
		}
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format of %s", mountinfoFile)
		}
		firstParts := strings.Fields(strings.TrimSpace(parts[0]))
		if len(firstParts) < 6 {
			return nil, fmt.Errorf("invalid format of %s", mountinfoFile)
		}
		mID, err := strconv.ParseUint(firstParts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid format of %s", mountinfoFile)
		}
		mountID := uint32(mID)

		pID, err := strconv.ParseUint(firstParts[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid format of %s", mountinfoFile)
		}
		parentID := uint32(pID)

		majorMinorParts := strings.Split(firstParts[2], ":")
		if len(majorMinorParts) != 2 {
			return nil, fmt.Errorf("invalid 'major:minor' format in %s", mountinfoFile)
		}
		majorNumber, err := strconv.ParseUint(majorMinorParts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse the major number in %s", mountinfoFile)
		}
		minorNumber, err := strconv.ParseUint(majorMinorParts[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse the minor number in %s", mountinfoFile)
		}

		mountRoot := firstParts[3]
		mountPoint := firstParts[4]
		mountOptions := firstParts[5]
		optionalFields := firstParts[6:]
		mountFlags := strings.Split(mountOptions, ",")

		secondParts := strings.Fields(strings.TrimSpace(parts[1]))
		if len(secondParts) < 3 {
			return nil, fmt.Errorf("invalid format of %s", mountinfoFile)
		}

		fsType := secondParts[0]
		mountSource := secondParts[1]
		superblockOptions := strings.Split(secondParts[2], ",")

		devName, partNum := splitDevAndPartNum(mountSource)

		mounts = append(mounts, MountInfo{
			Mountpoint:        mountPoint,
			MountFlags:        mountFlags,
			MountRoot:         mountRoot,
			MountID:           mountID,
			ParentID:          parentID,
			MountSource:       mountSource,
			SuperblockOptions: superblockOptions,
			FSType:            fsType,
			OptionalFields:    optionalFields,
			Major:             uint32(majorNumber),
			Minor:             uint32(minorNumber),
			DevName:           devName,
			PartitionNum:      uint(partNum),
		})
	}
	return mounts, nil
}
