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
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"k8s.io/klog"

	"github.com/minio/direct-csi/pkg/sys/loopback"
)

func readFirstLine(filename string, errorIfNotExist bool) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !errorIfNotExist {
			err = nil
		}
		return "", err
	}
	defer file.Close()
	s, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func readdirnames(dirname string, errorIfNotExist bool) ([]string, error) {
	dir, err := os.Open(dirname)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !errorIfNotExist {
			err = nil
		}
		return nil, err
	}
	defer dir.Close()
	return dir.Readdirnames(-1)
}

func getMajorMinor(name string) (major int, minor int, err error) {
	var majorMinor string
	if majorMinor, err = readFirstLine("/sys/class/block/"+name+"/dev", true); err != nil {
		return
	}

	tokens := strings.SplitN(majorMinor, ":", 2)
	if len(tokens) != 2 {
		err = fmt.Errorf("unknown format of %v", majorMinor)
		return
	}

	if major, err = strconv.Atoi(tokens[0]); err != nil {
		return
	}
	minor, err = strconv.Atoi(tokens[1])
	return
}

func getSize(name string) (uint64, error) {
	s, err := readFirstLine("/sys/class/block/"+name+"/size", true)
	if err != nil {
		return 0, err
	}
	ui64, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return ui64 * 512, nil
}

func getNGUID(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/nguid", false)
}

func getPartition(name string) (int, error) {
	s, err := readFirstLine("/sys/class/block/"+name+"/partition", false)
	if err != nil {
		return 0, err
	}
	if s != "" {
		return strconv.Atoi(s)
	}
	return 0, nil
}

func getRemovable(name string) (bool, error) {
	s, err := readFirstLine("/sys/class/block/"+name+"/removable", false)
	return s != "" && s != "0", err
}

func getReadOnly(name string) (bool, error) {
	s, err := readFirstLine("/sys/class/block/"+name+"/ro", false)
	return s != "" && s != "0", err
}

func getUUID(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/uuid", false)
}

func getWWID(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/wwid", false)
}

func getModel(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/device/model", false)
}

func getSerial(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/device/serial", false)
}

func getVendor(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/device/vendor", false)
}

func getDMName(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/dm/name", false)
}

func getDMUUID(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/dm/uuid", false)
}

func getMDUUID(name string) (string, error) {
	return readFirstLine("/sys/class/block/"+name+"/md/uuid", false)
}

func getVirtual(name string) (bool, error) {
	absPath, err := filepath.EvalSymlinks("/sys/class/block/" + name)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(absPath, "/sys/devices/virtual/block/"), nil
}

func getPartitions(name string) ([]string, error) {
	names, err := readdirnames("/sys/block/"+name, false)
	if err != nil {
		return nil, err
	}

	partitions := []string{}
	for _, n := range names {
		if strings.HasPrefix(n, name) {
			partitions = append(partitions, n)
		}
	}

	return partitions, nil
}

func getSlaves(name string) ([]string, error) {
	return readdirnames("/sys/block/"+name+"/slaves", false)
}

func readSysBlock() ([]string, error) {
	return readdirnames("/sys/block", true)
}

func readSysClassBlock() ([]string, error) {
	return readdirnames("/sys/class/block", true)
}

func getMountPoints() (map[string][]string, error) {
	file, err := os.Open("/proc/1/mountinfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)

	mountPointsMap := map[string][]string{}
	for {
		s, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		tokens := strings.Fields(strings.TrimSpace(s))
		if len(tokens) < 4 {
			return nil, fmt.Errorf("unknown format %v", strings.TrimSpace(s))
		}

		majorMinor := tokens[2]
		mountPoint := tokens[4]
		mountPointsMap[majorMinor] = append(mountPointsMap[majorMinor], mountPoint)
	}
	return mountPointsMap, nil
}

func probeDevices() (map[string]*Device, error) {
	names, err := readSysClassBlock()
	if err != nil {
		return nil, err
	}

	devices := map[string]*Device{}
	for _, name := range names {
		device := &Device{Name: name}
		if device.Major, device.Minor, err = getMajorMinor(name); err != nil {
			return nil, err
		}
		if device.Size, err = getSize(name); err != nil {
			return nil, err
		}
		if device.NGUID, err = getNGUID(name); err != nil {
			return nil, err
		}
		if device.Partition, err = getPartition(name); err != nil {
			return nil, err
		}
		if device.Removable, err = getRemovable(name); err != nil {
			return nil, err
		}
		if device.ReadOnly, err = getReadOnly(name); err != nil {
			return nil, err
		}
		if device.UUID, err = getUUID(name); err != nil {
			return nil, err
		}
		if device.WWID, err = getWWID(name); err != nil {
			return nil, err
		}
		if device.Model, err = getModel(name); err != nil {
			return nil, err
		}
		if device.Serial, err = getSerial(name); err != nil {
			return nil, err
		}
		if device.Vendor, err = getVendor(name); err != nil {
			return nil, err
		}
		if device.DMName, err = getDMName(name); err != nil {
			return nil, err
		}
		if device.DMUUID, err = getDMUUID(name); err != nil {
			return nil, err
		}
		if device.MDUUID, err = getMDUUID(name); err != nil {
			return nil, err
		}
		if device.Virtual, err = getVirtual(name); err != nil {
			return nil, err
		}
		devices[name] = device
	}

	if names, err = readSysBlock(); err != nil {
		return nil, err
	}
	for _, name := range names {
		partitions, err := getPartitions(name)
		if err != nil {
			return nil, err
		}
		for _, partition := range partitions {
			devices[partition].Parent = name
		}

		slaves, err := getSlaves(name)
		if err != nil {
			return nil, err
		}
		for _, slave := range slaves {
			devices[slave].Master = name
		}
	}

	mountPointsMap, err := getMountPoints()
	if err != nil {
		return nil, err
	}
	for _, device := range devices {
		device.MountPoints = mountPointsMap[fmt.Sprintf("%v:%v", device.Major, device.Minor)]
	}

	return devices, nil
}

func findDevices(ctx context.Context, loopBackOnly bool) ([]BlockDevice, map[string]*Device, error) {
	deviceMap, err := probeDevices()
	if err != nil {
		return nil, nil, err
	}

	var head = func() string {
		var deviceHead = "/sys/devices"
		if loopBackOnly {
			return filepath.Join(deviceHead, "virtual", "block")
		}
		return deviceHead
	}()

	drives := []BlockDevice{}
	var attachedLoopDeviceNames []string
	if loopBackOnly {
		var err error
		attachedLoopDeviceNames, err = loopback.GetAttachedDeviceNames()
		if err != nil {
			return drives, nil, err
		}
		if len(attachedLoopDeviceNames) == 0 {
			return drives, nil, fmt.Errorf("No loop devices attached")
		}
	}

	return drives, deviceMap, filepath.Walk(head, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !loopBackOnly && strings.HasPrefix(info.Name(), "loop") {
			return filepath.SkipDir
		}

		if info.Name() != "uevent" {
			return nil
		}
		drive, err := parseUevent(path)
		if err != nil {
			klog.V(5).Info(err)
			return nil
		}

		if loopBackOnly {
			if isAttachedDev := func() bool {
				for _, ldName := range attachedLoopDeviceNames {
					if ldName == drive.Devname {
						return true
					}
				}
				return false
			}(); isAttachedDev == false {
				return nil
			}
		}

		if drive.Devtype != "disk" {
			return nil
		}
		subsystem, err := subsystem(path)
		if err != nil {
			klog.V(5).Info(err)
			return nil
		}
		if subsystem != "block" {
			return nil
		}
		if err := drive.probeBlockDev(ctx, deviceMap); err != nil {
			klog.Errorf("Error while probing block device: %v", err)
		}

		drives = append(drives, *drive)
		return nil
	})
}

func getFirstMountInfo(mounts []MountInfo) (string, []string) {
	if len(mounts) > 0 {
		return mounts[0].Mountpoint, mounts[0].MountFlags
	}
	return "", nil
}

func FindDevices(ctx context.Context, loopBackOnly bool) (map[string]*Device, error) {
	blocks, deviceMap, err := findDevices(ctx, loopBackOnly)
	if err != nil {
		return nil, err
	}

	if loopBackOnly {
		loopDeviceMap := map[string]*Device{}
		for name, device := range deviceMap {
			if strings.HasPrefix(name, "loop") && device.Virtual {
				loopDeviceMap[name] = device
			}
		}
		deviceMap = loopDeviceMap
	}

	for _, block := range blocks {
		if _, found := deviceMap[block.Devname]; !found {
			continue
		}

		// FIXME: deviceMap[block.Devname].FSUUID = XXX
		deviceMap[block.Devname].FSType = block.FSType
		deviceMap[block.Devname].TotalCapacity = int64(block.FSInfo.TotalCapacity)
		deviceMap[block.Devname].FreeCapacity = int64(block.FSInfo.FreeCapacity)
		deviceMap[block.Devname].LogicalBlockSize = int64(block.LogicalBlockSize)
		deviceMap[block.Devname].PhysicalBlockSize = int64(block.PhysicalBlockSize)
		deviceMap[block.Devname].Path = block.Path
		if block.DeviceError != nil {
			deviceMap[block.Devname].Error = block.DeviceError.Error()
		}
		deviceMap[block.Devname].FirstMountPoint, deviceMap[block.Devname].FirstMountOptions = getFirstMountInfo(block.Mounts)

		for name := range deviceMap {
			if deviceMap[name].Parent != block.Devname {
				continue
			}

			for _, partition := range block.Partitions {
				if deviceMap[name].Partition != int(partition.PartitionNum) {
					continue
				}

				if deviceMap[deviceMap[name].Parent].PTUUID == "" {
					deviceMap[deviceMap[name].Parent].PTUUID = partition.DiskGUID
				}
				deviceMap[name].PartUUID = partition.PartitionGUID
				// FIXME: deviceMap[name].FSUUID = XXX
				deviceMap[name].FSType = partition.FSType
				deviceMap[name].TotalCapacity = int64(partition.FSInfo.TotalCapacity)
				deviceMap[name].FreeCapacity = int64(partition.FSInfo.FreeCapacity)
				deviceMap[name].LogicalBlockSize = int64(partition.LogicalBlockSize)
				deviceMap[name].PhysicalBlockSize = int64(partition.PhysicalBlockSize)
				deviceMap[name].Path = partition.Path
				deviceMap[name].FirstMountPoint, deviceMap[name].FirstMountOptions = getFirstMountInfo(partition.Mounts)
			}
		}
	}

	return deviceMap, nil
}

func (b *BlockDevice) GetPartitions() []Partition {
	return b.Partitions
}

func (b *BlockDevice) probeBlockDev(ctx context.Context, deviceMap map[string]*Device) (err error) {
	defer func() {
		if err != nil {
			b.TagError(err)
		}
	}()

	err = os.MkdirAll(DirectCSIDevRoot, 0755)
	if err != nil {
		return err
	}

	if b.DriveInfo == nil {
		return fmt.Errorf("Invalid drive info for %s", b.Devname)
	}

	devPath := b.DirectCSIDrivePath()
	err = makeBlockFile(devPath, b.DriveInfo.Major, b.DriveInfo.Minor)
	if err != nil {
		return err
	}

	b.Path = devPath
	var logicalBlockSize, physicalBlockSize uint64
	logicalBlockSize, physicalBlockSize, err = b.getBlockSizes()
	if err != nil {
		return err
	}
	b.LogicalBlockSize = logicalBlockSize
	b.PhysicalBlockSize = physicalBlockSize

	var driveSize uint64
	driveSize, err = b.getTotalCapacity()
	if err != nil {
		return err
	}
	b.TotalCapacity = driveSize

	numBlocks := driveSize / logicalBlockSize
	b.NumBlocks = numBlocks
	b.EndBlock = numBlocks

	var parts []Partition
	parts, err = b.probePartitions(ctx)
	if err != nil {
		if err != ErrNotPartition {
			return err
		}
	}

	b.DMName = deviceMap[b.Devname].DMName
	b.DMUUID = deviceMap[b.Devname].DMUUID
	b.Parent = deviceMap[b.Devname].Parent
	b.Master = deviceMap[b.Devname].Master
	for i := range parts {
		for name, drive := range deviceMap {
			if strings.HasPrefix(name, b.Devname) && drive.Parent == b.Devname && drive.Partition == int(parts[i].PartitionNum) {
				parts[i].DMName = drive.DMName
				parts[i].DMUUID = drive.DMUUID
				parts[i].Parent = drive.Parent
				parts[i].Master = drive.Master
			}
		}
	}

	if len(parts) == 0 {
		offsetBlocks := uint64(0)
		var fsInfo *FSInfo
		fsInfo, err = b.probeFS(offsetBlocks)
		if err != nil {
			if err != ErrNoFS {
				return err
			}
		}
		if fsInfo == nil {
			fsInfo = &FSInfo{
				TotalCapacity: b.TotalCapacity,
				FSBlockSize:   b.LogicalBlockSize,
			}
		}
		var mounts []MountInfo
		mounts, err = b.probeMountInfo(b.DriveInfo.Major, b.DriveInfo.Minor, deviceMap)
		if err != nil {
			return err
		}
		fsInfo.Mounts = append(fsInfo.Mounts, mounts...)
		b.FSInfo = fsInfo
		return nil
	}
	for _, p := range parts {
		offsetBlocks := p.StartBlock
		var fsInfo *FSInfo
		fsInfo, err = b.probeFS(offsetBlocks)
		if err != nil {
			if err != ErrNoFS {
				return err
			}
		}

		if fsInfo == nil {
			fsInfo = &FSInfo{
				TotalCapacity: p.TotalCapacity,
				FSBlockSize:   p.LogicalBlockSize,
			}
		}

		var mounts []MountInfo
		mounts, err = b.probeMountInfo(p.DriveInfo.Major, p.DriveInfo.Minor, deviceMap)
		if err != nil {
			return err
		}
		fsInfo.Mounts = append(fsInfo.Mounts, mounts...)
		p.FSInfo = fsInfo
		b.Partitions = append(b.Partitions, p)
	}
	return nil
}

func subsystem(path string) (string, error) {
	dir := filepath.Dir(path)
	link, err := os.Readlink(filepath.Join(dir, "subsystem"))
	if err != nil {
		return "", err
	}
	return filepath.Base(link), nil
}

func parseUevent(path string) (*BlockDevice, error) {
	if filepath.Base(path) != "uevent" {
		return nil, fmt.Errorf("not a uevent file")
	}

	uevent, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	uev := string(uevent)
	var major, minor, devname, devtype string

	lines := strings.Split(uev, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		cleanLine := strings.TrimSpace(line)
		parts := strings.Split(cleanLine, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("uevent file format not supported: %s", path)
		}
		key := parts[0]
		value := parts[1]
		switch key {
		case "MAJOR":
			major = value
		case "MINOR":
			minor = value
		case "DEVNAME":
			devname = value
		case "DEVTYPE":
			devtype = value
		default:
			return nil, fmt.Errorf("uevent file format not supported: %s", path)
		}
	}
	majorNum64, err := strconv.ParseUint(major, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid major num: %s", major)
	}
	minorNum64, err := strconv.ParseUint(minor, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid minor num: %s", minor)
	}
	majorNum := uint32(majorNum64)
	minorNum := uint32(minorNum64)

	return &BlockDevice{
		Devname: devname,
		Devtype: devtype,
		DriveInfo: &DriveInfo{
			Major: majorNum,
			Minor: minorNum,
		},
	}, nil
}

func (b *BlockDevice) getBlockSizes() (uint64, uint64, error) {
	devFile, err := os.OpenFile(b.DirectCSIDrivePath(), os.O_RDONLY, os.ModeDevice)
	if err != nil {
		return 0, 0, err
	}
	defer devFile.Close()

	fd := devFile.Fd()
	logicalBlockSize, err := unix.IoctlGetInt(int(fd), unix.BLKSSZGET)
	if err != nil {
		klog.Errorf("could not obtain logical block size for device: %s", b.Devname)
		return 0, 0, err
	}
	physicalBlockSize, err := unix.IoctlGetInt(int(fd), unix.BLKBSZGET)
	if err != nil {
		klog.Errorf("could not obtain physical block size for device: %s", b.Devname)
		return 0, 0, err
	}
	return uint64(logicalBlockSize), uint64(physicalBlockSize), nil
}

func (b *BlockDevice) getTotalCapacity() (uint64, error) {
	devFile, err := os.OpenFile(b.DirectCSIDrivePath(), os.O_RDONLY, os.ModeDevice)
	if err != nil {
		return 0, err
	}
	defer devFile.Close()

	driveSize, err := devFile.Seek(0, os.SEEK_END)
	if err != nil {
		return 0, err
	}
	return uint64(driveSize), nil
}
