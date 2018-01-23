package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	TabFileBeginMarker = "# -=-=- BEGIN PaaS AGENT MODIFICATIONS - DO NOT MODIFY! -=-=-"
	TabFileEndMarker   = "# -=-=- END PaaS AGENT MODIFICATIONS -=-=-"

	DeviceReplaceString  = "[device]"
	KeyFileReplaceString = "[keyFile]"

	devDir = "/dev"

	fstabFile    = "/etc/fstab"
	crypttabFile = "/etc/crypttab"

	tabFileMode   = 0644
	mountDirsMode = 0755

	mountKeyDirsMode = 0700
	mountKeyFileMode = 0600

	blockDevicePollInterval = time.Second
)

type HostMount struct {
	HostPath      string
	ContainerPath string
	BlockDevice   string

	Init      string
	FstabLine string

	CrypttabLine     string
	CryptInit        string
	CryptRemove      string
	KeyFile          string
	PostMountCommand string
}

func ConvertVolumes(kill <-chan struct{}, jobID string, volumes []JobVolume, mountKeysDir string) ([]HostMount, error) {
	var mounts []HostMount

	for i, v := range volumes {
		switch v.Type {
		case "simple":
			mounts = append(mounts, HostMount{
				HostPath:      v.HostPath,
				ContainerPath: v.ContainerPath,
			})
		case "block":
			lun := v.SCSILun
			if len(lun) > 0 {
				log.Info("Job %s: looking up block device for LUN %s", jobID, lun)
				device, err := GetBlockDeviceNameByLun(kill, lun)
				if err != nil {
					return nil, err
				}
				log.Info("Job %s: LUN %s corresponds to block device %s", jobID, lun, device)
				v.BlockDevice = path.Join(devDir, device)
				v.Init = strings.Replace(v.Init, DeviceReplaceString, device, -1)
				v.Crypttab = strings.Replace(v.Crypttab, DeviceReplaceString, device, -1)
				v.CryptInit = strings.Replace(v.CryptInit, DeviceReplaceString, device, -1)
				v.CryptRemove = strings.Replace(v.CryptRemove, DeviceReplaceString, device, -1)
			}
			key := v.CryptKey
			var keyFile string
			if len(key) > 0 {
				var err error
				keyFile, err = writeMountKey(mountKeysDir, jobID, i, key)
				if err != nil {
					return nil, fmt.Errorf("failed to write encryption key %d: %s", i, err)
				}
				v.Init = strings.Replace(v.Init, KeyFileReplaceString, keyFile, -1)
				v.Crypttab = strings.Replace(v.Crypttab, KeyFileReplaceString, keyFile, -1)
				v.CryptInit = strings.Replace(v.CryptInit, KeyFileReplaceString, keyFile, -1)
				v.CryptRemove = strings.Replace(v.CryptRemove, KeyFileReplaceString, keyFile, -1)
			}
			mounts = append(mounts, HostMount{
				HostPath:      v.HostPath,
				ContainerPath: v.ContainerPath,
				BlockDevice:   v.BlockDevice,

				Init:      v.Init,
				FstabLine: v.Fstab,

				CrypttabLine:     v.Crypttab,
				CryptInit:        v.CryptInit,
				CryptRemove:      v.CryptRemove,
				KeyFile:          keyFile,
				PostMountCommand: v.PostMountCommand,
			})
		default:
			return nil, fmt.Errorf("unsupported volume type: %s", v.Type)
		}
	}

	return mounts, nil
}

func generateBinds(mounts []HostMount) []string {
	var binds []string
	for _, m := range mounts {
		binds = append(binds, fmt.Sprintf("%s:%s", m.HostPath, m.ContainerPath))
	}
	return binds
}

func ConfigureHostMounts(kill <-chan struct{}, jobID string, mounts []HostMount) error {
	// wait for all block devices
	for _, v := range mounts {
		blockDevice := v.BlockDevice
		if len(blockDevice) > 0 {
			if err := waitForBlockDevice(kill, jobID, blockDevice); err != nil {
				return err
			}
		}
	}

	// initialize all mounts
	for _, v := range mounts {
		cryptInit := v.CryptInit
		if len(cryptInit) > 0 {
			log.Info("Job %s: crypt initializing mount at %s", jobID, v.HostPath)
			if err := invoke("/bin/sh", "-c", cryptInit); err != nil {
				return err
			}
		}
		init := v.Init
		if len(init) > 0 {
			log.Info("Job %s: initializing mount at %s", jobID, v.HostPath)
			if err := invoke("/bin/sh", "-c", init); err != nil {
				return err
			}
		}
	}

	// append lines to fstab and crypttab
	var fsLines []string
	var cryptLines []string
	for _, v := range mounts {
		line := v.FstabLine
		if len(line) > 0 {
			fsLines = append(fsLines, line)
		}
		cryptLine := v.CrypttabLine
		if len(cryptLine) > 0 {
			cryptLines = append(cryptLines, cryptLine)
		}
	}
	if fsLines != nil {
		if err := addTabLines(fsLines, fstabFile); err != nil {
			return err
		}
	}
	if cryptLines != nil {
		if err := addTabLines(cryptLines, crypttabFile); err != nil {
			return err
		}
	}

	// mount all new fstab entries
	for _, v := range mounts {
		line := v.FstabLine
		if len(line) > 0 {
			path := v.HostPath
			if len(path) > 0 {
				log.Info("Job %s: mounting %s", jobID, path)
				if err := os.MkdirAll(path, mountDirsMode); err != nil {
					return err
				}
				if err := mount(path); err != nil {
					return err
				}
				if len(v.PostMountCommand) > 0 {
					log.Info("Job %s: running post-mount command at %s", jobID, v.HostPath)
					if err := invoke("/bin/sh", "-c", v.PostMountCommand); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func CleanUpHostMounts(jobID string, mounts []HostMount) {
	// umount all tab file entries to be removed
	for _, v := range mounts {
		line := v.FstabLine
		if len(line) > 0 {
			path := v.HostPath
			if len(path) > 0 {
				log.Info("Job %s: unmounting %s", jobID, path)
				if err := unmount(path); err != nil {
					log.Errorf("Failed to unmount %s: %s", path, err)
				}
				if err := os.Remove(path); err != nil {
					log.Errorf("Failed to remove mount directory %s: %s", path, err)
				}
			}
		}
		cryptRemove := v.CryptRemove
		if len(cryptRemove) > 0 {
			if err := invoke("/bin/sh", "-c", cryptRemove); err != nil {
				log.Errorf("Failed to remove encrypted device %s: %s", cryptRemove, err)
			}
		}

		// remove mount encryption key
		if len(v.KeyFile) > 0 {
			if err := deleteMountKey(v.KeyFile); err != nil {
				log.Errorf("Failed to remove encryption key, \"%s\", for job %s: %s", v.KeyFile, jobID, err)
			}
		}
	}

	// remove lines from fstab
	var fsLines []string
	var cryptLines []string
	for _, v := range mounts {
		fsLine := v.FstabLine
		if len(fsLine) > 0 {
			fsLines = append(fsLines, fsLine)
		}
		cryptLine := v.CrypttabLine
		if len(cryptLine) > 0 {
			cryptLines = append(cryptLines, cryptLine)
		}
	}
	if fsLines != nil {
		if err := removeTabFileLines(fsLines, fstabFile); err != nil {
			log.Errorf("Failed to update %s: %s", fstabFile, err)
		}
	}
	if cryptLines != nil {
		if err := removeTabFileLines(cryptLines, crypttabFile); err != nil {
			log.Errorf("Failed to update %s: %s", crypttabFile, err)
		}
	}
}

func waitForBlockDevice(kill <-chan struct{}, jobID, blockDevice string) error {
	first := true
	for {
		exists, err := pathExists(blockDevice)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}

		if first {
			log.Info("Job %s: waiting for block device %s to appear on this host", jobID, blockDevice)
			first = false
		}
		select {
		case <-kill:
			log.Warn("Job %s was killed while waiting on a block device", jobID)
			return errJobKilled
		case <-time.After(blockDevicePollInterval):
		}
	}
}

func mount(hostPath string) error {
	return invoke("mount", hostPath)
}

func unmount(hostPath string) error {
	timeout := time.After(1 * time.Minute)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("failed to unmount %s within 1 minute", hostPath)
		default:
			err := invoke("umount", hostPath)
			if err == nil {
				return nil
			}
			log.Info("Failed to unmount %s - retrying: %s", hostPath, err)
			time.Sleep(5 * time.Second)
		}
	}
}

func resize(devPath string) error {
	return invoke("resize2fs", devPath)
}

// used to synchronize changes to the fstab file across goroutines
var tabMutex sync.Mutex

func addTabLines(lines []string, file string) error {
	tabMutex.Lock()
	defer tabMutex.Unlock()

	contents, err := readTabFile(file)
	if err != nil {
		return err
	}

	// append lines to file in the correct place
	var buf bytes.Buffer

	begin := strings.LastIndex(contents, TabFileBeginMarker)
	end := strings.LastIndex(contents, TabFileEndMarker)

	if begin >= 0 {
		if end < 0 {
			return fmt.Errorf("file has begin marker but no end marker")
		}
		// add new entries just before end marker
		buf.WriteString(contents[0:end])
		for _, line := range lines {
			buf.WriteString(line)
			buf.WriteRune('\n')
		}
		buf.WriteString(contents[end:])
	} else if end >= 0 {
		return fmt.Errorf("file has end but no begin marker")
	} else {
		// fresh file; add markers and new entries to end
		buf.WriteString(contents)
		buf.WriteString("\n")
		buf.WriteString(TabFileBeginMarker)
		buf.WriteRune('\n')
		for _, line := range lines {
			buf.WriteString(line)
			buf.WriteRune('\n')
		}
		buf.WriteString(TabFileEndMarker)
		buf.WriteRune('\n')
	}

	// write new file
	if err := writeTabFile(buf.Bytes(), file); err != nil {
		return err
	}

	log.Info("Added %s to %s", lines, file)
	return nil
}

func removeTabFileLines(lines []string, file string) error {
	tabMutex.Lock()
	defer tabMutex.Unlock()

	contents, err := readTabFile(file)
	if err != nil {
		return err
	}

	// remove lines from marked section
	begin := strings.LastIndex(contents, TabFileBeginMarker)
	end := strings.LastIndex(contents, TabFileEndMarker)

	if begin < 0 || end < 0 {
		return fmt.Errorf("file missing begin and/or end markers")
	}

	middle := contents[begin:end]
	for _, line := range lines {
		middle = strings.Replace(middle, line+"\n", "", 1)
	}

	var buf bytes.Buffer
	buf.WriteString(contents[0:begin])
	buf.WriteString(middle)
	buf.WriteString(contents[end:])

	// write new file
	if err := writeTabFile(buf.Bytes(), file); err != nil {
		return err
	}

	log.Info("Removed %s from %s", lines, file)
	return nil
}

func readTabFile(file string) (string, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeTabFile(data []byte, fileName string) error {
	tempFile := fileName + ".agent.new"
	file, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, tabFileMode)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := os.Rename(tempFile, fileName); err != nil {
		return err
	}
	return nil
}

// used to synchronize changes to the encryption key directory across goroutines
var mountKeyMutex sync.Mutex

func writeMountKey(mountKeysDir, jobID string, index int, keyBase64 string) (string, error) {
	mountKeyMutex.Lock()
	defer mountKeyMutex.Unlock()

	keyDir := path.Join(mountKeysDir, jobID)
	if err := os.MkdirAll(keyDir, mountKeyDirsMode); err != nil {
		return "", err
	}

	rawKey, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return "", err
	}

	keyFile := path.Join(keyDir, strconv.Itoa(index))
	return keyFile, ioutil.WriteFile(keyFile, rawKey, mountKeyFileMode)
}

func deleteMountKey(mountKey string) error {
	mountKeyMutex.Lock()
	defer mountKeyMutex.Unlock()
	return os.Remove(mountKey)
}
