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
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/golang/glog"
	direct_csi "github.com/minio/direct-csi/pkg/apis/direct.csi.min.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/mount"
)

type MountInfo struct {
	Mountpoint   string
	Filesystem   string
	MountOptions []string
}

func FindDrives(ctx context.Context, nodeID string, procfs string) ([]direct_csi.DirectCSIDrive, error) {
	idMap, err := getIDMap()
	if err != nil {
		return nil, err
	}

	mountInfoMap, err := getMountInfoMap(procfs)
	if err != nil {
		return nil, err
	}

	diskNames, err := getDiskNames()
	if err != nil {
		return nil, err
	}

	var drives []direct_csi.DirectCSIDrive

	for _, diskName := range diskNames {
		id, hasID := idMap[diskName]
		if !hasID {
			continue
		}

		drive, err := makeDrive(nodeID, diskName, "", id, mountInfoMap[diskName])
		if err != nil {
			return nil, err
		}

		drives = append(drives, *drive)

		partNames, err := getPartNames(diskName)
		if err != nil {
			return nil, err
		}

		for _, partName := range partNames {
			if strings.HasPrefix(partName, diskName) {
				id, hasID := idMap[partName]
				if !hasID {
					continue
				}

				drive, err = makeDrive(nodeID, diskName, partName, id, mountInfoMap[partName])
				if err != nil {
					return nil, err
				}

				drives = append(drives, *drive)
			}
		}
	}

	return drives, nil
}

func getIDMap() (map[string]string, error) {
	idMap := make(map[string]string)

	idsDir, err := os.Open("/dev/disk/by-id")
	defer idsDir.Close()
	if err != nil {
		return nil, err
	}

	ids, err := idsDir.Readdirnames(0)
	if err != nil {
		return nil, err
	}

	for _, id := range ids {
		dest, err := os.Readlink("/dev/disk/by-id/" + id)
		if err != nil {
			return nil, err
		}

		driveName := filepath.Base(dest)

		if previous, ok := idMap[driveName]; !ok || len(previous) > len(id) {
			idMap[driveName] = id
		}
	}

	return idMap, nil
}

func getMountInfoMap(procfs string) (map[string]MountInfo, error) {
	mountInfoMap := make(map[string]MountInfo)

	mounts, err := os.Open(procfs)
	defer mounts.Close()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(mounts)

	for scanner.Scan() {
		line := scanner.Text()
		words := strings.Split(line, " ")
		if len(words) < 6 {
			return nil, fmt.Errorf("Unrecognized mount format")
		}

		driveName := filepath.Base(words[0])
		if driveName != "" {
			// TODO (haslersn): What about multiple mount points?
			if _, ok := mountInfoMap[driveName]; !ok {
				mountInfoMap[driveName] = MountInfo{
					Mountpoint:   words[1],
					Filesystem:   words[2],
					MountOptions: strings.Split(words[3], ","),
				}
			}
		}
	}

	return mountInfoMap, nil
}

func getDiskNames() ([]string, error) {
	blockDir, err := os.Open("/sys/block/")
	defer blockDir.Close()
	if err != nil {
		return nil, err
	}

	return blockDir.Readdirnames(0)
}

func getPartNames(diskName string) ([]string, error) {
	driveDir, err := os.Open("/sys/block/" + diskName)
	defer driveDir.Close()
	if err != nil {
		return nil, err
	}

	return driveDir.Readdirnames(0)
}

func makeDrive(nodeID string, diskName string, partName string, serialNumber string, mountInfo MountInfo) (*direct_csi.DirectCSIDrive, error) {
	name, err := makeName(serialNumber)
	if err != nil {
		return nil, err
	}

	partNum, err := getPartNum(diskName, partName)
	if err != nil {
		return nil, err
	}

	model, err := getModel(diskName, partName)
	if err != nil {
		return nil, err
	}

	blockSize, err := getBlockSize(diskName, partName)
	if err != nil {
		return nil, err
	}

	blockCount, err := getBlockCount(diskName, partName)
	if err != nil {
		return nil, err
	}

	freeCapacity, err := getFreeCapacity(mountInfo)
	if err != nil {
		return nil, err
	}

	return &direct_csi.DirectCSIDrive{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		BlockSize:     blockSize,
		DriveStatus:   getDriveStatus(mountInfo.Filesystem),
		Filesystem:    mountInfo.Filesystem,
		FreeCapacity:  freeCapacity,
		ModelNumber:   model,
		MountOptions:  mountInfo.MountOptions,
		Mountpoint:    mountInfo.Mountpoint,
		OwnerNode:     nodeID,
		PartitionNum:  partNum,
		Path:          getPath(diskName, partName),
		RootPartition: diskName,
		SerialNumber:  serialNumber,
		TotalCapacity: blockSize * blockCount,
	}, nil

}

var dns1123AllowedCharsRegex = regexp.MustCompile(`[^-\.a-z0-9]+`)
var dns1123SubdomainRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

func makeName(input string) (string, error) {
	temp := dns1123AllowedCharsRegex.ReplaceAllString(strings.ToLower(input), "-")
	inputParts := strings.Split(temp, ".")
	var subdomainParts []string
	for _, inputPart := range inputParts {
		trimmed := strings.Trim(inputPart, "-")
		if trimmed != "" {
			subdomainParts = append(subdomainParts, trimmed)
		}
	}
	if len(subdomainParts) == 0 {
		return "", fmt.Errorf("Can not make a valid DNS-1123 subdomain from '%s'", input)
	}
	subdomain := strings.Join(subdomainParts, ".")
	if !dns1123SubdomainRegex.MatchString(subdomain) {
		panic(fmt.Errorf("makeName('%s') produced an invalid DNS-1123 subdomain '%s'", input, subdomain))
	}
	return subdomain, nil
}

func getPartNum(diskName string, partName string) (int, error) {
	if partName == "" {
		return 0, nil
	}

	data, err := ioutil.ReadFile(fmt.Sprintf("/sys/block/%s/%s/partition", diskName, partName))
	if err != nil {
		return 0, err
	} else {
		return strconv.Atoi(strings.TrimSpace(string(data)))
	}
}

func getModel(diskName string, partName string) (string, error) {
	data, err := ioutil.ReadFile(fmt.Sprintf("/sys/block/%s/%s/model", diskName, partName))
	if os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	} else {
		return strings.TrimSpace(string(data)), nil
	}
}

func getBlockSize(diskName string, partName string) (int64, error) {
	data, err := ioutil.ReadFile(fmt.Sprintf("/sys/block/%s/queue/logical_block_size", diskName))
	if err != nil {
		return 0, err
	} else {
		return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	}
}

func getBlockCount(diskName string, partName string) (int64, error) {
	data, err := ioutil.ReadFile(fmt.Sprintf("/sys/block/%s/%s/size", diskName, partName))
	if err != nil {
		return 0, err
	} else {
		return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	}
}

func getFreeCapacity(mountInfo MountInfo) (int64, error) {
	if mountInfo.Mountpoint == "" {
		return 0, nil
	}

	stat := &syscall.Statfs_t{}
	if err := syscall.Statfs(mountInfo.Mountpoint, stat); err != nil {
		return 0, err
	}
	return int64(stat.Bsize) * int64(stat.Bavail), nil
}

func getDriveStatus(filesystem string) direct_csi.DriveStatus {
	if filesystem == "" {
		return direct_csi.Unformatted
	} else {
		return direct_csi.New
	}
}

func getPath(diskName string, partName string) string {
	if partName == "" {
		return "/dev/" + diskName
	} else {
		return "/dev/" + partName
	}
}

// MountDevice - Utility to mount a device in the given mountpoint
func MountDevice(devicePath, mountPoint, fsType string, options []string) error {
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return err
	}
	if err := mount.New("").Mount(devicePath, mountPoint, fsType, options); err != nil {
		glog.V(5).Info(err)
		return err
	}
	return nil
}
