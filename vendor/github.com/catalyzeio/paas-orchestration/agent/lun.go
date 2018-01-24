package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	scsiDevicesDir = "/sys/bus/scsi/devices"
	sizeName       = "size"
	blockDirName   = "block"
	rescanName     = "rescan"

	// how long to wait for a rescan request to settle
	lunRescanDelay = 3 * time.Second
)

const (
	// waiting for a LUN device to appear
	lunStateWaiting = iota
	// waiting for a LUN rescan request to finish
	lunStateScanning
	// checking whether a LUN device is valid
	lunStateChecking
)

// Looks up the block device for a LUN device.
// The LUN name must be in colon-separated format, such as "5:0:0:0".
// This function will block until the LUN device is available, but can be
// killed by sending a signal to the kill channel.
func GetBlockDeviceNameByLun(kill <-chan struct{}, lun string) (string, error) {
	lunPath := path.Join(scsiDevicesDir, lun)
	state := lunStateWaiting
	for {
		if state == lunStateChecking {
			// Look up and validate the block device corresponding to the LUN.
			name, err := lunBlockDeviceName(lunPath)
			if err == nil {
				if len(name) > 0 {
					// valid LUN device; return block device name
					return name, nil
				}
				// LUN was invalid; force a rescan
				state = lunStateWaiting
			} else if os.IsNotExist(err) {
				// LUN disappeared; wait for it to come back
				log.Warn("LUN device %s disappeared when verifying", lun)
				state = lunStateWaiting
			} else {
				// some other error; abort
				return "", err
			}
		} else {
			// Initiate a rescan request.
			// As a side effect this also checks whether the LUN device is present.
			err := rescanLUN(lunPath)
			if err == nil {
				// present, wait for scan to finish
				state = lunStateScanning
			} else if os.IsNotExist(err) {
				// LUN is not yet present
				state = lunStateWaiting
			} else {
				// some other error; abort
				return "", err
			}
		}
		// wait before retrying the next operation
		delay := blockDevicePollInterval
		if state == lunStateScanning {
			delay = lunRescanDelay
		}
		select {
		case <-kill:
			log.Warn("Killed while getting block device for LUN %s", lun)
			return "", errJobKilled
		case <-time.After(delay):
		}
		// after a successful rescan proceed directly to check operation
		if state == lunStateScanning {
			state = lunStateChecking
		}
	}
}

// Initiates an OS-level rescan of the given SCSI LUN device.
// The caller is responsible for waiting an appropriate amount of time to let
// the rescan operation finish.
func rescanLUN(lunPath string) error {
	scanFile := path.Join(lunPath, rescanName)
	return ioutil.WriteFile(scanFile, []byte("1"), 0200)
}

// Looks up the block device name for the given SCSI LUN.
// Returns an empty string if the block device exists but is in an invalid state.
func lunBlockDeviceName(lunPath string) (string, error) {
	// look up block device name
	blockDir := path.Join(lunPath, blockDirName)
	blockFiles, err := ioutil.ReadDir(blockDir)
	if err != nil {
		return "", err
	}
	if len(blockFiles) != 1 {
		return "", fmt.Errorf("lunBlockDevice: directory %s had %d entries", blockDir, len(blockFiles))
	}
	deviceName := blockFiles[0].Name()
	// Validate that the block device is in a useable state.
	// Azure Hyper-V Linux VMs sometimes mistakenly report that a LUN block
	// device is ready for use when in reality it is not. This can be detected
	// by verifying that the LUN's size is not a bogus value.
	numBlocks, err := lunSize(lunPath, deviceName)
	if err != nil {
		return "", err
	}
	if numBlocks < 2 {
		log.Warn("LUN device %s reports a size of %d blocks; ignoring", lunPath, numBlocks)
		return "", nil
	}
	return deviceName, nil
}

// Gets the size of a SCSI LUN device as the number of 512-byte blocks.
func lunSize(lunPath, deviceName string) (int, error) {
	blockSizeFile := path.Join(lunPath, blockDirName, deviceName, sizeName)
	b, err := ioutil.ReadFile(blockSizeFile)
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(string(b))
	return strconv.Atoi(trimmed)
}
